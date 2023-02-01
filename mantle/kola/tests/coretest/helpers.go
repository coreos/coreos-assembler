package coretest

import (
	"bufio"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

func CheckPort(network, address string, timeout time.Duration) error {
	_, err := net.DialTimeout(network, address, timeout)
	return err
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

	buf, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	return strings.TrimSpace(string(buf))
}
