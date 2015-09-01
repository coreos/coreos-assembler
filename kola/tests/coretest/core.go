package coretest

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

const (
	CmdTimeout           = time.Second * 20
	DbusTimeout          = time.Second * 20
	DockerTimeout        = time.Second * 60
	PortTimeout          = time.Second * 3
	UpdateEnginePubKey   = "/usr/share/update_engine/update-payload-key.pub.pem"
	UpdateEnginePubKeyV1 = "d410d94dc56a1cba8df71c94ea6925811e44b09416f66958ab7a453f0731d80e"
	UpdateEnginePubKeyV2 = "a76a22e6afcdfbc55dd2953aa950c7ec93b254774fca02d13ec52c59672e5982"
)

func TestPortSsh() error {
	//t.Parallel()
	err := CheckPort("tcp", "127.0.0.1:22", PortTimeout)
	if err != nil {
		return err
	}
	return nil
}

func TestUpdateEngine() error {
	//t.Parallel()

	errc := make(chan error, 1)
	go func() {
		c := exec.Command("update_engine_client", "-status")
		err := c.Run()
		errc <- err
	}()

	select {
	case <-time.After(CmdTimeout):
		return fmt.Errorf("update_engine_client timed out after %s.", CmdTimeout)
	case err := <-errc:
		if err != nil {
			return err
		}
		return nil
	}

	// FIXME(marineam): Test DBus directly
}

func TestDockerEcho() error {
	//t.Parallel()
	errc := make(chan error, 1)
	go func() {
		c := exec.Command("docker", "run", "busybox", "echo")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(DockerTimeout):
		return fmt.Errorf("DockerEcho timed out after %s.", DockerTimeout)
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("DockerEcho: %v", err)
		}
		return nil
	}
}

func TestDockerPing() error {
	//t.Parallel()
	errc := make(chan error, 1)
	go func() {
		c := exec.Command("docker", "run", "busybox", "ping", "-c4", "coreos.com")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(DockerTimeout):
		return fmt.Errorf("DockerPing timed out after %s.", DockerTimeout)
	case err := <-errc:
		if err != nil {
			return err
		}
		return nil
	}
}

func TestNTPDate() error {
	//t.Parallel()
	errc := make(chan error, 1)
	go func() {
		c := exec.Command("ntpdate", "-d", "-s", "-u", "pool.ntp.org")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(CmdTimeout):
		return fmt.Errorf("ntpdate timed out after %s.", CmdTimeout)
	case err := <-errc:
		if err != nil {
			return err
		}
		return nil
	}
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

func TestSymlinkResolvConf() error {
	//t.Parallel()
	f, err := os.Lstat("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("SymlinkResolvConf: %v", err)
	}
	if !IsLink(f) {
		return fmt.Errorf("/etc/resolv.conf is not a symlink.")
	}
	return nil
}

func TestInstalledUpdateEngineRsaKeys() error {
	//t.Parallel()
	fileHash, err := Sha256File(UpdateEnginePubKey)
	if err != nil {
		return err
	}

	switch string(fileHash) {
	case UpdateEnginePubKeyV1, UpdateEnginePubKeyV2:
		return nil
	default:
		return fmt.Errorf("%s:%s unexpected hash.", UpdateEnginePubKey, fileHash)
	}
}

func TestServicesActive() error {
	//t.Parallel()
	units := []string{
		"multi-user.target",
		"docker.socket",
		"systemd-timesyncd.service",
		"update-engine.service",
	}
	for _, unit := range units {
		c := exec.Command("systemctl", "is-active", unit)
		err := c.Run()
		if err != nil {
			return fmt.Errorf("Services Active: %v", err)
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

// Test "Add User Manually", from https://coreos.com/os/docs/latest/adding-users.html
func TestUseradd() error {
	c := exec.Command("sudo", "useradd", "-p", "*", "-U", "-m", "user1", "-G", "sudo")
	err := c.Run()
	if err != nil {
		return fmt.Errorf("useradd: %v", err)
	}

	// verify
	_, err = user.Lookup("user1")
	if err != nil {
		return fmt.Errorf("user.Lookup: %v", err)
	}

	return nil
}
