package coretest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	CmdTimeout               = time.Second * 20
	DbusTimeout              = time.Second * 20
	DockerTimeout            = time.Second * 60
	PortTimeout              = time.Second * 3
	UpdateEnginePubKey       = "/usr/share/update_engine/update-payload-key.pub.pem"
	UpdateEnginePubKeySha256 = "d410d94dc56a1cba8df71c94ea6925811e44b09416f66958ab7a453f0731d80e"
)

func TestPortSsh(t *testing.T) {
	t.Parallel()
	err := CheckPort("tcp", "127.0.0.1:22", PortTimeout)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateEngine(t *testing.T) {
	t.Parallel()

	errc := make(chan error, 1)
	go func() {
		c := exec.Command("update_engine_client", "-status")
		err := c.Run()
		errc <- err
	}()

	select {
	case <-time.After(CmdTimeout):
		t.Fatalf("update_engine_client timed out after %s.", CmdTimeout)
	case err := <-errc:
		if err != nil {
			t.Error(err)
		}
	}

	// FIXME(marineam): Test DBus directly
}

func TestDockerEcho(t *testing.T) {
	t.Parallel()
	errc := make(chan error, 1)
	go func() {
		c := exec.Command("docker", "run", "busybox", "echo")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(DockerTimeout):
		t.Fatalf("DockerEcho timed out after %s.", DockerTimeout)
	case err := <-errc:
		if err != nil {
			t.Error(err)
		}
	}
}

func TestNTPDate(t *testing.T) {
	t.Parallel()
	errc := make(chan error, 1)
	go func() {
		c := exec.Command("ntpdate", "-d", "-s", "-u", "pool.ntp.org")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(CmdTimeout):
		t.Fatalf("ntpdate timed out after %s.", CmdTimeout)
	case err := <-errc:
		if err != nil {
			t.Error(err)
		}
	}
}

// This execs gdbus, because we need to change uses to test perms.
func TestDbusPerms(t *testing.T) {
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
		if !strings.Contains(string(out), "org.freedesktop.DBus.Error.AccessDenied") {
			t.Error(err)
		}
	} else {
		t.Error("We were able to call RestartUnit as a non-root user.")
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
		t.Error(string(out))
		t.Error(err)
	}
}

func TestSymlinkResolvConf(t *testing.T) {
	t.Parallel()
	f, err := os.Lstat("/etc/resolv.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !IsLink(f) {
		t.Fatal("/etc/resolv.conf is not a symlink.")

	}
}

func TestInstalledUpdateEngineRsaKeys(t *testing.T) {
	t.Parallel()
	fileHash, err := Sha256File(UpdateEnginePubKey)
	if err != nil {
		t.Fatal(err)
	}

	if string(fileHash) != UpdateEnginePubKeySha256 {
		t.Fatalf("%s:%s does not match hash %s.", UpdateEnginePubKey, fileHash,
			UpdateEnginePubKeySha256)
	}
}

func TestServicesActive(t *testing.T) {
	t.Parallel()
	units := []string{
		"default.target",
		"docker.socket",
		"ntpd.service",
		"update-engine.service",
	}
	for _, unit := range units {
		c := exec.Command("systemctl", "is-active", unit)
		err := c.Run()
		if err != nil {
			t.Error(err)
		}
	}
}

func TestReadOnlyFs(t *testing.T) {
	mountModes := make(map[string]bool)
	mounts, err := GetMountTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mounts {
		mountModes[m.MountPoint] = m.Options[0] == "ro"
	}
	if mp, ok := mountModes["/usr"]; ok {
		if mp {
			return
		} else {
			t.Fatal("/usr is not mounted read-only.")
		}
	} else if mp, ok := mountModes["/"]; ok {
		if mp {
			return
		} else {
			t.Fatal("/ is not mounted read-only.")
		}
	}
	t.Fatal("could not find /usr or / mount points.")
}
