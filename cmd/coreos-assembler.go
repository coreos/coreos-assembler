// This is the primary entrypoint for /usr/bin/coreos-assembler.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
)

// commands we'd expect to use in the local dev path
var buildCommands = []string{"init", "fetch", "build", "run", "prune", "clean", "list"}
var advancedBuildCommands = []string{"buildfetch", "buildupload", "oc-adm-release", "push-container", "upload-oscontainer", "buildextend-extensions"}
var buildextendCommands = []string{"aliyun", "applehv", "aws", "azure", "digitalocean", "exoscale", "extensions", "extensions-container", "gcp", "hashlist-experimental", "hyperv", "ibmcloud", "kubevirt", "legacy-oscontainer", "live", "metal", "metal4k", "nutanix", "openstack", "qemu", "secex", "virtualbox", "vmware", "vultr"}

var utilityCommands = []string{"aws-replicate", "compress", "copy-container", "koji-upload", "kola", "push-container-manifest", "remote-build-container", "remote-prune", "remote-session", "sign", "tag", "update-variant"}
var otherCommands = []string{"shell", "meta"}

func init() {
	// Note buildCommands is intentionally listed in frequency order
	sort.Strings(advancedBuildCommands)
	sort.Strings(buildextendCommands)
	sort.Strings(utilityCommands)
	sort.Strings(otherCommands)
}

func wrapCommandErr(err error) error {
	if err == nil {
		return nil
	}
	if exiterr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("%w\n%s", err, exiterr.Stderr)
	}
	return err
}

func printCommands(title string, cmds []string) {
	fmt.Printf("%s:\n", title)
	var prefix string
	if title == "Platform builds" {
		prefix = "buildextend-"
	}
	for _, cmd := range cmds {
		fmt.Printf("  %s%s\n", prefix, cmd)
	}
}

func printUsage() {
	fmt.Println("Usage: coreos-assembler CMD ...")
	printCommands("Build commands", buildCommands)
	printCommands("Advanced build commands", advancedBuildCommands)
	printCommands("Platform builds", buildextendCommands)
	printCommands("Utility commands", utilityCommands)
	printCommands("Other commands", otherCommands)
}

func run(argv []string) error {
	if err := initializeGlobalState(argv); err != nil {
		return fmt.Errorf("failed to initialize global state: %w", err)
	}

	var cmd string
	if len(argv) > 0 {
		cmd = argv[0]
		argv = argv[1:]
	}

	if cmd == "" || cmd == "--help" {
		printUsage()
		os.Exit(1)
	}

	// if the COREOS_ASSEMBLER_REMOTE_SESSION environment variable is
	// set then we "intercept" the command here and redirect it to
	// `cosa remote-session exec`, which will execute the commands
	// via `podman --remote` on a remote machine.
	session, ok := os.LookupEnv("COREOS_ASSEMBLER_REMOTE_SESSION")
	if ok && session != "" && cmd != "remote-session" {
		argv = append([]string{"exec", "--", cmd}, argv...)
		cmd = "remote-session"
	}

	// Manual argument parsing here for now; once we get to "phase 1"
	// of the Go conversion we can vendor cobra (and other libraries)
	// at the toplevel.
	switch cmd {
	case "clean":
		return runClean(argv)
	case "update-variant":
		return runUpdateVariant(argv)
	case "remote-session":
		return runRemoteSession(argv)
	case "build-extensions-container", // old alias
		"buildextend-extensions-container":
		return buildExtensionContainer()
	}

	target := fmt.Sprintf("/usr/lib/coreos-assembler/cmd-%s", cmd)
	_, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("unknown command: %s", cmd)
		}
		return fmt.Errorf("failed to stat %s: %w", target, err)
	}

	c := exec.Command(target, argv...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to execute cmd-%s: %v\n", cmd, err.Error())
		return err
	}
	return nil
}

func initializeGlobalState(argv []string) error {
	// Set PYTHONUNBUFFERED=1 so that we get unbuffered output. We should
	// be able to do this on the shebang lines but env doesn't support args
	// right now. In Fedora we should be able to use the `env -S` option.
	os.Setenv("PYTHONUNBUFFERED", "1")

	// docker/podman don't run through PAM, but we want this set for the privileged
	// (non-virtualized) path
	user, ok := os.LookupEnv("USER")
	if !ok {
		b, err := exec.Command("id", "-nu").Output()
		if err == nil {
			user = strings.TrimSpace(string(b))
		} else {
			user = "cosa"
		}
		os.Setenv("USER", user)
	}

	// https://github.com/containers/libpod/issues/1448
	// if /sys/fs/selinux is mounted, various tools will think they're on a SELinux enabled
	// host system, and we don't want that.  Work around this by overmounting it.
	// So far we only see /sys/fs/selinux mounted in a privileged container, so we know we
	// have privileges to create a new mount namespace and overmount it with an empty directory.
	const selinuxfs = "/sys/fs/selinux"
	if _, err := os.Stat(selinuxfs + "/status"); err == nil {
		const unsharedKey = "coreos_assembler_unshared"
		if _, ok := os.LookupEnv(unsharedKey); ok {
			err := exec.Command("sudo", "mount", "--bind", "/usr/share/empty", "/sys/fs/selinux").Run()
			if err != nil {
				return fmt.Errorf("failed to unmount %s: %w", selinuxfs, wrapCommandErr(err))
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: %s appears to be mounted but should not be; enabling workaround\n", selinuxfs)
			selfpath, err := os.Readlink("/proc/self/exe")
			if err != nil {
				return err
			}
			baseArgv := []string{"sudo", "-E", "--", "env", fmt.Sprintf("%s=1", unsharedKey), "unshare", "-m", "--", "runuser", "-u", user, "--", selfpath}
			err = syscall.Exec("/usr/bin/sudo", append(baseArgv, argv...), os.Environ())
			return fmt.Errorf("failed to re-exec self to unmount %s: %w", selinuxfs, err)
		}
	}

	// When trying to connect to libvirt we get "Failed to find user record
	// for uid" errors if there is no entry for our UID in /etc/passwd.
	// This was taken from 'Support Arbitrary User IDs' section of:
	//   https://docs.openshift.com/container-platform/3.10/creating_images/guidelines.html
	c := exec.Command("whoami")
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "notice: failed to look up uid in /etc/passwd; enabling workaround")
		home := fmt.Sprintf("/var/tmp/%s", user)
		err := os.MkdirAll(home, 0755)
		if err != nil {
			return err
		}
		f, err := os.OpenFile("/etc/passwd", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("opening /etc/passwd: %w", err)
		}
		defer f.Close()
		id := os.Getuid()
		buf := fmt.Sprintf("%s:x:%d:0:%s user:%s:/sbin/nologin\n", user, id, user, home)
		if _, err = f.WriteString(buf); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	err := run(os.Args[1:])
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// In this case the command we ran gave a non-zero exit
			// code. Let's also exit with that exit code.
			os.Exit(exitErr.ExitCode())
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
