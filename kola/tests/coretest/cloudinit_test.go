package coretest

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"testing"
)

const cloudinitBinPath = "/usr/bin/coreos-cloudinit"

func read(filename string) (string, error) {
	bytes, err := ioutil.ReadFile(filename)
	return string(bytes), err
}

func rmdir(path string) error {
	cmd := exec.Command("sudo", "rm", "-rf", path)
	return cmd.Run()
}

func TestCloudinitCloudConfig(t *testing.T) {
	workspace, err := ioutil.TempDir("", "coretest-cloudinit-")
	if err != nil {
		t.Fatalf("Failed creating workspace: %v", err)
	}
	defer rmdir(workspace)

	keyOne := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC5LaGMGRqZEEvOhHlIEiQgdMJIQ9Qe8L/XSz06GqzcESbEnYLIXar2nou4eW4AGMVC1V0BrcWWnSTxM1/dWeCLOUt5NulKAjtdBUZGhCT83nbimSzbmx3/q2y5bCiS4Zr8ZjYFbi1eLvye2jKPE4xo7cvIfDKc0ztQ9kU7JknUdKNZo3RKXr5EPhJ5UZ8Ff15CI9+hDSvdPwer+HNnEt/psRVC+s29EwNGwUXD4IYqrk3X4ew0YAl/oULHM4cctoBW9GM+kAl40rOuIARlKfe4UdCgDMHYA/whi7Us+cPNgPit9IVJVBU4eo/cF5molD2l+PMSntypuv79obu8sA1H cloudinit-test-key-one"
	keyTwo := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCZw5Ljtt9wlEfyDvmUwu/BeMcIhVarbcM4ajZolxRy9G8vvCa7ODcSjzSyhfG1mLSBB2KfaFFI6zGHBjFX0Gzy9i8m3u7PnZBPX30bb1n0hJCrUhpqUGQUe8OFdoBstf1HIwJU/KoTBL0Ap1WEn0quRT4kNgBLbPrMjYCPbS1q4wJKdIE5rRm/EfTUrmIb0i91gujEGw5oUHDXf0X+/cxwwIVZh1z16YhOgvJBzXhsJ9a0w7kcy/6wPRv03yyMg/r2Ada6ci68LulKz5GLn+xInT0bvIcra/PZ7WE+jyZhZKly239VZyT/1dHkBbTw+kgnGobLMbjOOg5bKaT8NZJ3 cloudinit-test-key-two"

	configTmpl := `#cloud-config
coreos:
    etcd:
        discovery: https://discovery.etcd.io/827c73219eeb2fa5530027c37bf18877
ssh_authorized_keys:
    - %s
    - %s
`
	configData := fmt.Sprintf(configTmpl, keyOne, keyTwo)
	configFile, err := ioutil.TempFile(os.TempDir(), "coretest-")
	if err != nil {
		t.Fatalf("Failed creating tempfile: %v", err)
	}
	defer syscall.Unlink(configFile.Name())

	if _, err := io.WriteString(configFile, configData); err != nil {
		t.Fatalf("Failed writing %s: %v", configFile.Name(), err)
	}

	if stdout, stderr, err := Run("sudo", cloudinitBinPath, "--workspace", workspace, "--from-file", configFile.Name(), "--ssh-key-name", "coretest"); err != nil {
		t.Fatalf("coreos-cloudinit failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	contents, err := read("/run/systemd/system/etcd.service.d/20-cloudinit.conf")
	if err != nil {
		t.Errorf("Unable to read etcd bootstrap file: %v", err)
	} else if !strings.Contains(contents, "Environment=\"ETCD_DISCOVERY=https://discovery.etcd.io/827c73219eeb2fa5530027c37bf18877\"") {
		t.Errorf("Incorrect data written to etcd.service.d/20-cloudinit.conf: %s", contents)
	}

	// Attempt to clean up after ourselves
	defer Run("update-ssh-keys", "-d", "coretest")

	authorized_keys, err := read("/home/core/.ssh/authorized_keys")
	if err != nil {
		t.Fatalf("Unable to read authorized_keys file: %v", err)
	}

	if !strings.Contains(authorized_keys, keyOne) {
		t.Errorf("Could not find first key in authorized_keys")
	}

	if !strings.Contains(authorized_keys, keyTwo) {
		t.Errorf("Could not find second key in authorized_keys")
	}
}

func TestCloudinitScript(t *testing.T) {
	workspace, err := ioutil.TempDir("", "coretest-cloudinit-")
	if err != nil {
		t.Fatalf("Failed creating workspace: %v", err)
	}
	defer rmdir(workspace)

	configData := `#!/bin/bash
/bin/sleep 10
`
	configFile, err := ioutil.TempFile(os.TempDir(), "coretest-")
	if err != nil {
		t.Fatalf("Failed creating tempfile: %v", err)
	}
	defer syscall.Unlink(configFile.Name())

	if _, err := io.WriteString(configFile, configData); err != nil {
		t.Fatalf("Failed writing %s: %v", configFile.Name(), err)
	}

	if stdout, stderr, err := Run("sudo", cloudinitBinPath, "--workspace", workspace, "--from-file", configFile.Name()); err != nil {
		t.Fatalf("coreos-cloudinit failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	unitName, err := read(path.Join(workspace, "scripts", "unit-name"))
	if err != nil {
		t.Fatalf("Unable to read unit name from cloudinit workspace: %v", err)
	}
	defer Run("systemctl", "stop", unitName)

	stdout, stderr, err := Run("systemctl", "status", unitName)
	if err != nil {
		t.Fatalf("Unable to determine if user-data was executed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "Active: active") {
		t.Errorf("User-data unit is not active")
	}
}
