package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"testing"
	"time"
)

const (
	CaPath      = "/usr/share/coreos-ca-certificates/"
	CmdTimeout  = time.Second * 3
	PortTimeout = time.Second * 3
)

func IsLink(f os.FileInfo) bool {
	return f.Mode()&os.ModeSymlink != 0
}

func CheckPort(network, address string, timeout time.Duration) error {
	errc := make(chan error)
	go func() {
		_, err := net.Dial(network, address)
		errc <- err
	}()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s:%s timed out after %s seconds.",
			network, address, timeout)
	case err := <-errc:
		if err != nil {
			return err
		}
	}
	return nil
}

func TestPortSsh(t *testing.T) {
	t.Parallel()
	err := CheckPort("tcp", "127.0.0.1:22", PortTimeout)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateEngine(t *testing.T) {

}

func TestDockerEcho(t *testing.T) {
	t.Parallel()
	errc := make(chan error)
	go func() {
		c := exec.Command("docker", "run", "busybox", "echo")
		err := c.Run()
		errc <- err
	}()
	select {
	case <-time.After(CmdTimeout):
		t.Fatalf("DockerEcho timed out after %s.", CmdTimeout)
	case err := <-errc:
		if err != nil {
			t.Error(err)
		}
	}
}

func TestUpdateServiceHttp(t *testing.T) {

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

func TestInstalledCACerts(t *testing.T) {
	t.Parallel()
	caCerts := []string{
		"CoreOS_Internet_Authority.pem",
		"CoreOS_Network_Authority.pem",
	}
	for _, fileName := range caCerts {
		_, err := os.Stat(path.Join(CaPath, fileName))
		if err != nil {
			t.Error(err)
		}
	}
}

func TestInstalledUpdateEngineRsaKeys(t *testing.T) {

}
