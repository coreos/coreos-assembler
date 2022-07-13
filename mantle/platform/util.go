// Copyright 2015 CoreOS, Inc.
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

package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Manhole connects os.Stdin, os.Stdout, and os.Stderr to an interactive shell
// session on the Machine m. Manhole blocks until the shell session has ended.
// If os.Stdin does not refer to a TTY, Manhole returns immediately with a nil
// error.
func Manhole(m Machine) (err error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}

	tstate, _ := term.MakeRaw(fd)
	defer func() {
		e := term.Restore(fd, tstate)
		if err != nil {
			err = fmt.Errorf("%v; %v", err, e)
		} else {
			err = e
		}
	}()

	client, err := m.SSHClient()
	if err != nil {
		return fmt.Errorf("SSH client failed: %v", err)
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH session failed: %v", err)
	}

	defer session.Close()

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	modes := ssh.TerminalModes{
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}

	cols, lines, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}

	if err = session.RequestPty(os.Getenv("TERM"), lines, cols, modes); err != nil {
		return errors.Wrapf(err, "failed to request pseudo terminal")
	}

	if err := session.Shell(); err != nil {
		return errors.Wrapf(err, "failed to start shell")
	}

	if err := session.Wait(); err != nil {
		return errors.Wrapf(err, "failed to wait for session")
	}

	return nil
}

// Reboots a machine, stopping ssh first.
// Afterwards run CheckMachine to verify the system is back and operational.
func StartReboot(m Machine) error {
	out, stderr, err := m.SSH("sudo reboot")
	if _, ok := err.(*ssh.ExitMissingError); ok {
		// A terminated session is perfectly normal during reboot.
		err = nil
	}
	if err != nil {
		return fmt.Errorf("issuing reboot command failed: %s: %s: %s", out, err, stderr)
	}
	return nil
}

// RebootMachine will reboot a given machine, provided the machine's journal.
func RebootMachine(m Machine, j *Journal) error {
	bootId, err := GetMachineBootId(m)
	if err != nil {
		return err
	}
	if err := StartReboot(m); err != nil {
		return fmt.Errorf("machine %q failed to begin rebooting: %v", m.ID(), err)
	}
	return StartMachineAfterReboot(m, j, bootId)
}

// WaitForMachineReboot will wait for the machine to reboot, i.e. it is assumed
// an action which will cause a reboot has already been initiated. Note the
// timeout here is for how long to wait for the machine to seemingly go
// *offline*, not for how long it takes to get back online. Journal.Start() has
// its own timeouts for that.
func WaitForMachineReboot(m Machine, j *Journal, timeout time.Duration, oldBootId string) error {
	// The machine could be in three distinct states here wrt SSH
	// accessibility: it may be up before the reboot, or down during the
	// reboot, or up after the reboot.

	// we *require* the old boot ID, otherwise there's no way to know if the
	// machine already rebooted
	if oldBootId == "" {
		panic("unreachable: oldBootId empty")
	}

	// Run a command that stays blocked until we're killed by the reboot process
	// on the target machine, so we know we know approximately when the reboot happens.
	// This is intended to be "best effort", there are a wide variety of potential
	// failure modes.  In the future we should probably have a relatively short
	// timeout here and not make it fatal, and instead only do a timeout inside StartMachineAfterReboot().
	c := make(chan error)
	go func() {
		out, stderr, err := m.SSH(fmt.Sprintf("if [ $(cat /proc/sys/kernel/random/boot_id) == '%s' ]; then echo waiting for reboot | logger && sleep infinity; fi", oldBootId))
		if err == nil {
			// we're already in the new boot!
			c <- nil
		} else if _, ok := err.(*ssh.ExitMissingError); ok {
			// we got interrupted running the command, likely by sshd going down
			c <- nil
		} else if strings.Contains(err.Error(), "connection reset by peer") {
			// Catch ECONNRESET, which can happen if sshd is killed during
			// handshake. crypto/ssh doesn't provide a distinct error type for
			// this, so we're left looking for the string... :(
			c <- nil
		} else if strings.Contains(err.Error(), "handshake failed: EOF") {
			// This can also happen if we're killed during handshake
			c <- nil
		} else if strings.Contains(err.Error(), "dial tcp") {
			// Catch "dial tcp xx.xx.xx.xx:xx: connect: connection refused"
			// error which occcurs when rebooting on online platforms.
			c <- nil
		} else {
			c <- fmt.Errorf("waiting for reboot failed: %s: %s: %s", out, err, stderr)
		}
	}()

	select {
	case err := <-c:
		if err != nil {
			return err
		}
		return StartMachineAfterReboot(m, j, oldBootId)
	case <-time.After(timeout):
		return fmt.Errorf("timed out after %v waiting for machine to reboot", timeout)
	}
}

func StartMachineAfterReboot(m Machine, j *Journal, oldBootId string) error {
	if err := j.Start(context.TODO(), m, oldBootId); err != nil {
		return fmt.Errorf("machine %q failed to start: %v", m.ID(), err)
	}
	if err := CheckMachine(context.TODO(), m); err != nil {
		return fmt.Errorf("machine %q failed basic checks: %v", m.ID(), err)
	}
	return nil
}

// StartMachine will start a given machine, provided the machine's journal.
func StartMachine(m Machine, j *Journal) error {
	errchan := make(chan error)
	go func() {
		err := m.IgnitionError()
		if err != nil {
			msg := fmt.Sprintf("machine %s entered emergency.target in initramfs", m.ID())
			plog.Info(msg)
			path := filepath.Join(filepath.Dir(j.journalPath), "ignition-virtio-dump.txt")
			if err := ioutil.WriteFile(path, []byte(err.Error()), 0644); err != nil {
				plog.Errorf("Failed to write journal: %v", err)
			}
			errchan <- errors.New(msg)
		}
	}()
	go func() {
		// This one ends up connecting to the journal via ssh
		errchan <- StartMachineAfterReboot(m, j, "")
	}()
	return <-errchan
}

func GetMachineBootId(m Machine) (string, error) {
	stdout, stderr, err := m.SSH("cat /proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("failed to retrieve boot ID: %s: %s: %s", stdout, err, stderr)
	}
	return strings.TrimSpace(string(stdout)), nil
}

// GenerateFakeKey generates a SSH key pair, returns the public key, and
// discards the private key. This is useful for droplets that don't need a
// public key, since DO & Azure insists on requiring one.
func GenerateFakeKey() (string, error) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	sshKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return "", err
	}
	return string(ssh.MarshalAuthorizedKey(sshKey)), nil
}
