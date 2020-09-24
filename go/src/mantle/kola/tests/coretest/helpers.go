package coretest

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"
)

func CheckPort(network, address string, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		_, err := net.Dial(network, address)
		errc <- err
	}()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s:%s timed out after %s seconds.",
			network, address, timeout)
	case err := <-errc:
		return err
	}
}

type MountTable struct {
	Device     string
	MountPoint string
	FsType     string
	Options    []string
}

func GetMountTable() (mounts []MountTable, err error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.Split(scanner.Text(), " ")
		if len(line) < 6 {
			continue
		}
		mounts = append(mounts, MountTable{
			Device:     line[0],
			MountPoint: line[1],
			FsType:     line[2],
			Options:    strings.Split(line[3], ","),
		})
	}
	return
}

// MachineID returns the content of /etc/machine-id. It panics on any error.
func MachineID() string {
	f, err := os.Open("/etc/machine-id")
	if err != nil {
		panic(err)
	}

	defer f.Close()

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}

	return strings.TrimSpace(string(buf))
}
