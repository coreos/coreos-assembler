package coretest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pborman/uuid"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/util"
)

const (
	DockerTimeout = time.Second * 60
	PortTimeout   = time.Second * 3
)

// RHCOS services we expect disabled/inactive
var offServices = []string{
	"dnsmasq.service",
	"iscsid.service",
	"iscsid.socket",
	"iscsiuio.service",
	"nfs-blkmap.service",
	"nfs-idmapd.service",
	"nfs-mountd.service",
	"nfs-server.service",
	"nis-domainname.service",
	"rbdmap.service",
	"rdisc.service",
	"rpc-statd.service",
	"rpcbind.service",
	"rpcbind.socket",
	"tcsd.service",
}

func init() {
	register.RegisterTest(&register.Test{
		Name:        "basic",
		Run:         LocalTests,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"PortSSH":        register.CreateNativeFuncWrap(TestPortSsh),
			"DbusPerms":      register.CreateNativeFuncWrap(TestDbusPerms),
			"NetworkScripts": register.CreateNativeFuncWrap(TestNetworkScripts, []string{"s390x"}...),
			"ServicesActive": register.CreateNativeFuncWrap(TestServicesActive),
			"ReadOnly":       register.CreateNativeFuncWrap(TestReadOnlyFs),
			"Useradd":        register.CreateNativeFuncWrap(TestUseradd),
			"MachineID":      register.CreateNativeFuncWrap(TestMachineID),
			"RHCOSGrowpart":  register.CreateNativeFuncWrap(TestRHCOSGrowfs, []string{"fcos"}...),
			"FCOSGrowpart":   register.CreateNativeFuncWrap(TestFCOSGrowfs, []string{"rhcos"}...),
		},
	})
	// TODO: Enable DockerPing/DockerEcho once fixed
	// TODO: Only enable PodmanPing on non qemu-unpriv. Needs:
	// https://github.com/coreos/mantle/issues/1132
	register.RegisterTest(&register.Test{
		Name:        "fcos.internet",
		Run:         InternetTests,
		ClusterSize: 1,
		Flags:       []register.Flag{register.RequiresInternetAccess},
		NativeFuncs: map[string]register.NativeFuncWrap{
			"PodmanEcho":     register.CreateNativeFuncWrap(TestPodmanEcho),
			"PodmanWgetHead": register.CreateNativeFuncWrap(TestPodmanWgetHead),
		},
		Distros: []string{"fcos"},
	})
	register.RegisterTest(&register.Test{
		Name:        "rootfs.uuid",
		Run:         LocalTests,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"RandomUUID": register.CreateNativeFuncWrap(TestFsRandomUUID),
		},
		// FIXME run on RHCOS once it has https://github.com/coreos/ignition-dracut/pull/93
		Distros: []string{"fcos"},
	})
	register.RegisterTest(&register.Test{
		Name:        "rhcos.services-disabled",
		Run:         LocalTests,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"ServicesDisabled": register.CreateNativeFuncWrap(TestServicesDisabledRHCOS),
		},
		Distros: []string{"rhcos"},
	})
}

func TestPortSsh() error {
	//t.Parallel()
	err := CheckPort("tcp", "127.0.0.1:22", PortTimeout)
	if err != nil {
		return err
	}
	return nil
}

func TestDockerEcho() error {
	//t.Parallel()
	return util.RunCmdTimeout(DockerTimeout, "docker", "run", "busybox", "echo")
}

func TestDockerPing() error {
	//t.Parallel()
	return util.RunCmdTimeout(DockerTimeout, "docker", "run", "busybox", "ping", "-c4", "coreos.com")
}

func TestPodmanEcho() error {
	//t.Parallel()
	return util.RunCmdTimeout(DockerTimeout, "podman", "run", "busybox", "echo")
}

func TestPodmanPing() error {
	//t.Parallel()
	return util.RunCmdTimeout(DockerTimeout, "podman", "run", "busybox", "ping", "-c4", "coreos.com")
}

func TestPodmanWgetHead() error {
	//t.Parallel()
	return util.RunCmdTimeout(DockerTimeout, "podman", "run", "busybox", "wget", "--spider", "http://fedoraproject.org/static/hotspot.txt")
}

// This execs gdbus, because we need to change uses to test perms.
func TestDbusPerms() error {
	c := exec.Command(
		"sudo", "-u", "core",
		"gdbus", "call", "--system",
		"--dest", "org.freedesktop.systemd1",
		"--object-path", "/org/freedesktop/systemd1",
		"--method", "org.freedesktop.systemd1.Manager.RestartUnit",
		"ntpd.service", "replace",
	)
	out, err := c.CombinedOutput()

	if err != nil {
		if !strings.Contains(string(out), "org.freedesktop.DBus.Error.AccessDenied") &&
			!strings.Contains(string(out), "org.freedesktop.DBus.Error.InteractiveAuthorizationRequired") {
			return err
		}
	} else {
		return fmt.Errorf("We were able to call RestartUnit as a non-root user.")
	}

	c = exec.Command(
		"sudo", "-u", "core",
		"gdbus", "call", "--system",
		"--dest", "org.freedesktop.systemd1",
		"--object-path", "/org/freedesktop/systemd1/unit/ntpd_2eservice",
		"--method", "org.freedesktop.DBus.Properties.GetAll",
		"org.freedesktop.systemd1.Unit",
	)

	out, err = c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Err:%s\n Out:%v", err, out)
	}
	return nil
}

func TestServicesActive() error {
	return servicesActive([]string{
		"multi-user.target",
	})
}

func servicesActive(units []string) error {
	//t.Parallel()
	for _, unit := range units {
		c := exec.Command("systemctl", "is-active", unit)
		err := c.Run()
		if err != nil {
			return fmt.Errorf("Services Active: %v", err)
		}
	}
	return nil
}

func TestServicesDisabledRHCOS() error {
	err := servicesInactive(offServices)
	if err != nil {
		return err
	}

	err = servicesDisabled(offServices)
	if err != nil {
		return err
	}
	return nil
}

func servicesInactive(units []string) error {
	for _, unit := range units {
		c := exec.Command("systemctl", "is-active", unit)
		err := c.Run()
		if err == nil {
			return fmt.Errorf("Service Incorrectly Active: %q", unit)
		}
	}
	return nil
}

func servicesDisabled(units []string) error {
	for _, unit := range units {
		c := exec.Command("systemctl", "is-enabled", unit)
		out, err := c.Output()
		if err == nil {
			// "is-enabled" can return 0 in some cases when the output is not
			// explicitly "disabled".  In the case of the RHCOS services
			// that are checked, we expect some to report "static"
			outString := strings.TrimSuffix(string(out), "\n")
			if (outString != "disabled") && (outString != "static") {
				return fmt.Errorf("Service Incorrectly Enabled: %q", unit)
			}
		}
	}
	return nil
}

func TestReadOnlyFs() error {
	mountModes := make(map[string]bool)
	mounts, err := GetMountTable()
	if err != nil {
		return err
	}
	for _, m := range mounts {
		mountModes[m.MountPoint] = m.Options[0] == "ro"
	}
	if mp, ok := mountModes["/usr"]; ok {
		if mp {
			return nil
		} else {
			return fmt.Errorf("/usr is not mounted read-only.")
		}
	} else if mp, ok := mountModes["/"]; ok {
		if mp {
			return nil
		} else {
			return fmt.Errorf("/ is not mounted read-only.")
		}
	}
	return fmt.Errorf("could not find /usr or / mount points.")
}

func TestNetworkScripts() error {
	networkScriptsDir := "/etc/sysconfig/network-scripts"
	entries, err := ioutil.ReadDir(networkScriptsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if len(entries) > 0 {
		return fmt.Errorf("Found content in %s", networkScriptsDir)
	}
	return nil
}

// Test that the root disk's GUID was set to a random one on first boot.
func TestFsRandomUUID() error {
	c := exec.Command("sh", "-ec", "sudo blkid -o value -s PTUUID /dev/$(lsblk -no PKNAME $(findmnt -vno SOURCE /))")
	out, err := c.Output()
	if err != nil {
		return fmt.Errorf("findmnt: %v", err)
	}

	got, err := uuid.ParseBytes(bytes.TrimSpace(out))
	if err != nil {
		return fmt.Errorf("malformed GUID: %v", err)
	}

	defaultGUID := uuid.Parse("00000000-0000-4000-a000-000000000001")
	if uuid.Equal(defaultGUID, got) {
		return fmt.Errorf("unexpected default GUID found")
	}

	return nil
}

// Test "Add User Manually", from https://coreos.com/os/docs/latest/adding-users.html
func TestUseradd() error {
	u := "user1"
	c := exec.Command("sudo", "useradd", "-p", "*", "-U", "-m", u, "-G", "sudo")
	err := c.Run()
	if err != nil {
		return fmt.Errorf("useradd: %v", err)
	}

	// verify
	c = exec.Command("id", u)
	err = c.Run()
	if err != nil {
		return fmt.Errorf("id %s: %v", u, err)
	}

	return nil
}

// Test that /etc/machine-id isn't empty or COREOS_BLANK_MACHINE_ID
func TestMachineID() error {
	id := MachineID()
	if id == "" {
		return fmt.Errorf("machine-id is empty")
	} else if id == "COREOS_BLANK_MACHINE_ID" {
		return fmt.Errorf("machine-id is %s", id)
	}
	return nil
}

func testGrowfs(size int) error {
	// check that ignition-ostree-growfs.service was run and exited normally
	err := checkService("ignition-ostree-growfs.service")
	if err != nil {
		return err
	}
	err = checkFilesystemSize(size)
	if err != nil {
		return err
	}
	return nil
}

// TestRHCOSGrowfs tests whether ignition-ostree-growfs.service was run
// successfully and check that filesystem size has been grown to at least 15
// GB.
func TestRHCOSGrowfs() error {
	// check that filesystem size is >= 15 GB
	return testGrowfs(15 * 1024 * 1024 * 1024)
}

// TestFCOSGrowfs tests whether ignition-ostree-growfs.service was run successfully
// and check that filesystem size has been grown to at least 7 GB.
func TestFCOSGrowfs() error {
	// check that filesystem size is >= 7 GB
	return testGrowfs(7 * 1024 * 1024 * 1024)
}

func checkService(unit string) error {
	// Value of MESSAGE_ID= is the SD_MESSAGE_UNIT_STARTED macro's value from
	// https://github.com/systemd/systemd/blob/master/src/systemd/sd-messages.h
	// For oneshot type services that remain after exit, STARTED being "done"
	// should imply that the service ran and exited successfully.
	c := exec.Command("journalctl", "-o", "json", "MESSAGE_ID=39f53479d3a045ac8e11786248231fbf",
		"UNIT="+unit)
	out, err := c.Output()
	if err != nil {
		return fmt.Errorf("journalctl: %s", err)
	}
	if len(out) == 0 {
		return fmt.Errorf("%s did not start", unit)
	}
	var journalOutput map[string]string
	if err := json.Unmarshal(out, &journalOutput); err != nil {
		return fmt.Errorf("Error getting journalclt output for %s: %s. Out: %s", unit, err, out)
	}
	if journalOutput["JOB_RESULT"] != "done" {
		return fmt.Errorf("%s did not start successfully\n Journalctl output: %q", unit, out)
	}
	return nil
}

func checkFilesystemSize(size int) error {
	c := exec.Command("bash", "-c", "echo $(($(stat -f /sysroot --format '%S * %b')))")
	filesystemSizeStr, err := c.Output()
	if err != nil {
		return fmt.Errorf("Error getting filesystem size: %s", err)
	}
	filesystemSize, err := strconv.Atoi(strings.TrimSuffix(string(filesystemSizeStr), "\n"))
	if err != nil {
		return fmt.Errorf("Error converting filesystem size from string to int: %s", err)
	}
	if filesystemSize < size {
		return fmt.Errorf("Filesystem size is less than %d bytes, size in bytes: %d", size, filesystemSize)
	}
	return nil
}
