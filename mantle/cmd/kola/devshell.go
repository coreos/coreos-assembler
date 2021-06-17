// Copyright 2020 Red Hat, Inc.
//
// Run qemu as a development shell
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

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/platform"
)

const devshellHostname = "cosa-devsh"

func devshellSSH(configPath, keyPath string, args ...string) exec.Cmd {
	sshArgs := append([]string{"-i", keyPath, "-F", configPath, devshellHostname}, args...)
	sshCmd := exec.Command("ssh", sshArgs...)
	sshCmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	return *sshCmd
}

func readTrimmedLine(r *bufio.Reader) (string, error) {
	l, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(l), nil
}

func stripControlCharacters(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if !strconv.IsGraphic(r) {
			return ' '
		}
		return r
	}, s)
}

func displaySerialMsg(serialMsg string) {
	s := strings.TrimSpace(serialMsg)
	if s == "" {
		return
	}
	max := 100
	if len(s) > max {
		s = s[:max]
	}
	fmt.Printf("\033[2K\r%s", stripControlCharacters(s))
}

func runDevShellSSH(ctx context.Context, builder *platform.QemuBuilder, conf *conf.Conf) error {
	if !terminal.IsTerminal(0) {
		return fmt.Errorf("stdin is not a tty")
	}
	tmpd, err := ioutil.TempDir("", "kola-devshell")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)
	sshPubKeyBuf, sshKeyPath, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}

	// Await sshd startup;
	// src/systemd/sd-messages.h
	// 89:#define SD_MESSAGE_UNIT_STARTED           SD_ID128_MAKE(39,f5,34,79,d3,a0,45,ac,8e,11,78,62,48,23,1f,bf)
	journalPipe, err := builder.VirtioJournal(conf, "-u sshd MESSAGE_ID=39f53479d3a045ac8e11786248231fbf")
	if err != nil {
		return err
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	conf.AddAuthorizedKeys("core", keys)

	readyReader := bufio.NewReader(journalPipe)

	builder.SetConfig(conf)

	serialChan := make(chan string)
	serialPipe, err := builder.SerialPipe()
	if err != nil {
		return err
	}
	serialLog, err := ioutil.TempFile("", "cosa-run-serial")
	if err != nil {
		return err
	}
	go func() {
		bufr := bufio.NewReader(serialPipe)
		for {
			buf, err := bufr.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(os.Stderr, "devshell reading serial console: %v\n", err)
				}
				break
			}
			serialChan <- string(buf)
		}
	}()

	builder.InheritConsole = false
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()

	qemuWaitChan := make(chan error)
	errchan := make(chan error)
	readychan := make(chan struct{})
	go func() {
		buf, err := inst.WaitIgnitionError(ctx)
		if err != nil {
			errchan <- err
		} else {
			// TODO parse buf and try to nicely render something
			if buf != "" {
				errchan <- platform.ErrInitramfsEmergency
			}
		}
	}()
	go func() {
		qemuWaitChan <- inst.Wait()
	}()
	go func() {
		for {
			readyMsg, err := readTrimmedLine(readyReader)
			if err != nil {
				if err != io.EOF {
					errchan <- err
				}
				break
			}
			if !strings.Contains(readyMsg, "Started OpenSSH server daemon") {
				errchan <- fmt.Errorf("Unexpected journal message: %s", readyMsg)
				break
			}
			var s struct{}
			readychan <- s
			break
		}
	}()
	sigintChan := make(chan os.Signal, 1)
	signal.Notify(sigintChan, os.Interrupt)

loop:
	for {
		select {
		case err := <-errchan:
			if err == platform.ErrInitramfsEmergency {
				return fmt.Errorf("instance failed in initramfs; try rerunning with --devshell-console")
			}
			return err
		case err := <-qemuWaitChan:
			return errors.Wrapf(err, "qemu exited before setup")
		case serialMsg := <-serialChan:
			displaySerialMsg(serialMsg)
			if _, err := serialLog.Write([]byte(serialMsg)); err != nil {
				return err
			}
		case <-sigintChan:
			_, err := serialLog.Seek(0, io.SeekStart)
			if err != nil {
				return err
			}
			_, err = io.Copy(os.Stderr, serialLog)
			if err != nil {
				return err
			}
			// Caught SIGINT, we're done
			return fmt.Errorf("Caught SIGINT before successful login")
		case <-readychan:
			fmt.Printf("\033[2K\rvirtio journal connected - sshd started\n")
			break loop
		}
	}

	// Later Ctrl-c after this should just kill us
	signal.Reset(os.Interrupt)

	// This one takes ownership of the readyReader
	poweroffStarted := false
	go func() {
		msg, _ := readTrimmedLine(readyReader)
		if msg == "poweroff" {
			poweroffStarted = true
		}
	}()


	// Ignore other status messages, and just print errors for now
	go func() {
		for {
			select {
			case <-serialChan:
			case <-readychan:
			case err := <-errchan:
				fmt.Fprintf(os.Stderr, "errchan: %v", err)
			}
		}
	}()

	var ip string
	err = util.Retry(6, 5*time.Second, func() error {
		var err error
		ip, err = inst.SSHAddress()
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "awaiting ssh address")
	}

	sshConfigPath := filepath.Join(tmpd, "ssh-config")
	sshConfig, err := os.OpenFile(sshConfigPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrapf(err, "creating ssh config")
	}
	defer sshConfig.Close()
	sshBuf := bufio.NewWriter(sshConfig)

	_, err = fmt.Fprintf(sshBuf, "Host %s\n", devshellHostname)
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(ip)
	if err != nil {
		// Yeah this is hacky, surprising there's not a stdlib API for this
		host = ip
		port = ""
	}
	if port != "" {
		if _, err := fmt.Fprintf(sshBuf, "  Port %s\n", port); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(sshBuf, `  HostName %s
	  StrictHostKeyChecking no
	  UserKnownHostsFile /dev/null
	  User core
	  PasswordAuthentication no
	  KbdInteractiveAuthentication no
	  GSSAPIAuthentication no
	  IdentitiesOnly yes
	  ForwardAgent no
	  ForwardX11 no
	`, host); err != nil {
		return err
	}

	if err := sshBuf.Flush(); err != nil {
		return err
	}

	err = util.Retry(10, 1*time.Second, func() error {
		cmd := devshellSSH(sshConfigPath, sshKeyPath, "true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ssh failed: %w\n%s", err, out)
		}
		return nil
	})
	if err != nil {
		return err
	}

	go func() {
		for {
			// FIXME figure out how to differentiate between reboot/shutdown
			// if poweroffStarted {
			// 	break
			// }
			cmd := devshellSSH(sshConfigPath, sshKeyPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Println("Disconnected, attempting to reconnect (Ctrl-C to exit)")
				time.Sleep(1 * time.Second)
			} else {
				proc := os.Process{
					Pid: inst.Pid(),
				}
				poweroffStarted = true
				if err := proc.Signal(os.Interrupt); err != nil {
					fmt.Println("Failed to power off")
				}
				break
			}
		}
	}()
	err = <-qemuWaitChan
	if err == nil {
		if !poweroffStarted {
			fmt.Println("QEMU powered off unexpectedly")
		}
	}
	return err
}
