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
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	v3 "github.com/coreos/ignition/v2/config/v3_0"
	v3types "github.com/coreos/ignition/v2/config/v3_0/types"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
)

const devshellHostname = "cosa-devsh"

func devshellSSH(configPath, keyPath string, silent bool, args ...string) exec.Cmd {
	sshArgs := append([]string{"-i", keyPath, "-F", configPath, devshellHostname}, args...)
	sshCmd := exec.Command("ssh", sshArgs...)
	if !silent {
		sshCmd.Stdin = os.Stdin
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr
	}
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

func runDevShellSSH(builder *platform.QemuBuilder, conf *v3types.Config) error {
	if !terminal.IsTerminal(0) {
		return fmt.Errorf("stdin is not a tty")
	}

	tmpd, err := ioutil.TempDir("", "kola-devshell")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	sshKeyPath := filepath.Join(tmpd, "ssh.key")
	sshPubKeyPath := sshKeyPath + ".pub"
	err = exec.Command("ssh-keygen", "-N", "", "-t", "ed25519", "-f", sshKeyPath).Run()
	if err != nil {
		return errors.Wrapf(err, "running ssh-keygen")
	}
	sshPubKeyBuf, err := ioutil.ReadFile(sshPubKeyPath)
	if err != nil {
		return errors.Wrapf(err, "reading pubkey")
	}
	sshPubKey := v3types.SSHAuthorizedKey(strings.TrimSpace(string(sshPubKeyBuf)))

	readinessSignalChan := "coreos.devshellready"
	signalReadyUnit := fmt.Sprintf(`[Unit]
Requires=dev-virtio\\x2dports-%s.device
OnFailure=emergency.target
OnFailureJobMode=isolate
After=systemd-user-sessions.service
After=sshd.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/echo ready
StandardOutput=file:/dev/virtio-ports/%[1]s
[Install]
RequiredBy=multi-user.target`, readinessSignalChan)
	signalPoweroffUnit := fmt.Sprintf(`[Unit]
Requires=dev-virtio\\x2dports-%s.device
OnFailure=emergency.target
OnFailureJobMode=isolate
Conflicts=reboot.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStop=/bin/echo poweroff
StandardOutput=file:/dev/virtio-ports/%[1]s
[Install]
WantedBy=multi-user.target`, readinessSignalChan)

	devshellConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Passwd: v3types.Passwd{
			Users: []v3types.PasswdUser{
				{
					Name: "core",
					SSHAuthorizedKeys: []v3types.SSHAuthorizedKey{
						sshPubKey,
					},
				},
			},
		},
		Systemd: v3types.Systemd{
			Units: []v3types.Unit{
				{
					Name:     "coreos-devshell-signal-ready.service",
					Contents: &signalReadyUnit,
					Enabled:  util.BoolToPtr(true),
				},
				{
					Name:     "coreos-devshell-signal-poweroff.service",
					Contents: &signalPoweroffUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
	confm := v3.Merge(*conf, devshellConfig)
	conf = &confm

	readyChan, err := builder.VirtioChannelRead(readinessSignalChan)
	if err != nil {
		return err
	}
	readyReader := bufio.NewReader(readyChan)

	builder.SetConfig(*conf, kola.Options.IgnitionVersion == "v2")

	serialPipe, err := builder.SerialPipe()
	if err != nil {
		return err
	}
	serialLog, err := ioutil.TempFile("", "cosa-run-serial")
	if err != nil {
		return err
	}
	os.Remove(serialLog.Name())
	serialTee := io.TeeReader(serialPipe, serialLog)

	builder.InheritConsole = false
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()

	statusChan, statusErrChan := inst.ParseSerialConsoleState(serialTee)
	qemuWaitChan := make(chan error)
	errchan := make(chan error)
	readychan := make(chan struct{})
	go func() {
		// Just proxy this one
		err := <-statusErrChan
		errchan <- err
	}()
	go func() {
		buf, err := inst.WaitIgnitionError()
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
		readyMsg, err := readTrimmedLine(readyReader)
		if err != nil {
			errchan <- err
		}
		if readyMsg != "ready" {
			errchan <- fmt.Errorf("Unexpected ready message: %s", readyMsg)
		}
		var s struct{}
		readychan <- s
	}()

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
		case status := <-statusChan:
			fmt.Printf("\033[2K\rstate: %s", status)
		case _ = <-readychan:
			fmt.Printf("\033[2K\rvirtio: connected\n")
			break loop
		}
	}

	// Ignore other status messages, and just print errors for now
	go func() {
		for {
			select {
			case _ = <-statusChan:
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
		cmd := devshellSSH(sshConfigPath, sshKeyPath, true, "true")
		return cmd.Run()
	})
	if err != nil {
		return err
	}

	poweroffStarted := false
	go func() {
		msg, _ := readTrimmedLine(readyReader)
		if msg == "poweroff" {
			poweroffStarted = true
		}
	}()

	go func() {
		for {
			// FIXME figure out how to differentiate between reboot/shutdown
			// if poweroffStarted {
			// 	break
			// }
			cmd := devshellSSH(sshConfigPath, sshKeyPath, false)
			if err := cmd.Run(); err != nil {
				fmt.Println("Disconnected, attempting to reconnect (Ctrl-C to exit)")
				time.Sleep(1 * time.Second)
			} else {
				proc := os.Process{
					Pid: inst.Pid(),
				}
				poweroffStarted = true
				proc.Signal(os.Interrupt)
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
