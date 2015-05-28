package coretest

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
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

func IsLink(f os.FileInfo) bool {
	return f.Mode()&os.ModeSymlink != 0
}

func CheckHttpStatus(url string, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		tr := &http.Transport{}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			errc <- err
			return
		}

		resp, err := tr.RoundTrip(req)
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			errc <- fmt.Errorf("%s failed with status %d.", url, resp.StatusCode)
			return
		}
		errc <- nil
	}()

	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s timed out after %s seconds.",
			url, timeout)
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

func Sha256File(fileName string) (hash string, err error) {
	f, err := os.Open(fileName)
	if err != nil {
		return
	}

	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return
	}

	fileHasher := sha256.New()
	fileHasher.Write(bytes)
	return hex.EncodeToString(fileHasher.Sum(nil)), nil
}

func Run(command string, args ...string) (string, string, error) {
	var stdoutBytes, stderrBytes bytes.Buffer
	cmd := exec.Command(command, args...)
	cmd.Stdout = &stdoutBytes
	cmd.Stderr = &stderrBytes
	err := cmd.Run()
	return stdoutBytes.String(), stderrBytes.String(), err
}
