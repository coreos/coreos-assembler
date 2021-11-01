// Copyright 2018 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/pkg/errors"
)

const (
	LITTLE Endian = iota // little endian
	BIG                  // big endian
)

// Endianness of the platform - big or little
type Endian int

var HostEndianness Endian

func init() {
	// Determine endianness - https://stackoverflow.com/questions/51332658/any-better-way-to-check-endianness-in-go
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0x0100)

	switch buf {
	case [2]byte{0x00, 0x01}:
		HostEndianness = LITTLE
	case [2]byte{0x01, 0x00}:
		HostEndianness = BIG
	default:
		HostEndianness = LITTLE
	}
}

func StrToPtr(s string) *string {
	return &s
}

func BoolToPtr(b bool) *bool {
	return &b
}

func IntToPtr(i int) *int {
	return &i
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateSSHAuthorizedKey generates a public key to sanity check
// that Ignition accepts it.
func CreateSSHAuthorizedKey(tmpd string) ([]byte, string, error) {
	var err error
	sshKeyPath := filepath.Join(tmpd, "ssh.key")
	sshPubKeyPath := sshKeyPath + ".pub"
	err = exec.Command("ssh-keygen", "-N", "", "-t", "ed25519", "-f", sshKeyPath).Run()
	if err != nil {
		return nil, "", errors.Wrapf(err, "running ssh-keygen")
	}
	sshPubKeyBuf, err := ioutil.ReadFile(sshPubKeyPath)
	if err != nil {
		return nil, "", errors.Wrapf(err, "reading pubkey")
	}
	return sshPubKeyBuf, sshKeyPath, nil
}

// RunCmdTimeout runs a command but returns an error if it doesn't complete
// before the given duration.
func RunCmdTimeout(timeout time.Duration, cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	err := c.Start()
	if err != nil {
		return err
	}

	errc := make(chan error, 1)
	go func() {
		errc <- c.Wait()
	}()

	select {
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("%s: %v", cmd, err)
		}
		return nil
	case <-time.After(timeout):
		// this uses the waitid(WNOWAIT) trick to avoid racing:
		// https://github.com/golang/go/commit/cea29c4a358004d84d8711a07628c2f856b381e8
		c.Process.Kill()
		<-errc
		return fmt.Errorf("%s timed out after %s", cmd, timeout)
	}
}
