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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

const devshellHostname = "cosa-devsh"

func stripControlCharacters(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if !strconv.IsGraphic(r) {
			return ' '
		}
		return r
	}, s)
}

func displayStatusMsg(status, msg string) {
	s := strings.TrimSpace(msg)
	if s == "" {
		return
	}
	max := 100
	if len(s) > max {
		s = s[:max]
	}
	fmt.Printf("\033[2K\r[%s] %s", status, stripControlCharacters(s))
}

func runDevShellSSH(ctx context.Context, builder *platform.QemuBuilder, conf *conf.Conf, sshCommand string) error {
	if !terminal.IsTerminal(0) {
		return fmt.Errorf("stdin is not a tty")
	}

	tmpd, err := ioutil.TempDir("", "kola-devshell")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	// Define SSH key
	sshPubKeyBuf, sshKeyPath, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}
	keys := []string{strings.TrimSpace(string(sshPubKeyBuf))}
	conf.AddAuthorizedKeys("core", keys)
	builder.SetConfig(conf)

	// errChan communicates errors from go routines
	errChan := make(chan error)
	// serialChan is used to send serial messages from the console
	serialChan := make(chan string, 2048)
	// stateChan reports in-instance state such as shutdown, reboot, etc.
	stateChan := make(chan guestState)

	watchJournal(builder, conf, stateChan, errChan)

	// SerialPipe is the pipe output from the serial console.
	serialPipe, err := builder.SerialPipe()
	if err != nil {
		return err
	}
	serialLog, err := ioutil.TempFile(tmpd, "cosa-run-serial")
	if err != nil {
		return err
	}

	builder.InheritConsole = false
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()

	// Monitor for Inition error
	go func() {
		buf, err := inst.WaitIgnitionError(ctx)
		if err != nil {
			errChan <- err
		} else if buf != "" {
			errChan <- platform.ErrInitramfsEmergency
		}
	}()

	// Monitor the serial console for ready and power state events.
	go func() {
		serialScanner := bufio.NewScanner(serialPipe)
		for serialScanner.Scan() {
			msg := serialScanner.Text()
			if serialScanner.Err() != nil {
				errChan <- err
			}

			_, _ = serialLog.WriteString(fmt.Sprintf("%s\n", msg))
			serialChan <- msg
			checkWriteState(msg, stateChan)
		}
	}()

	qemuWaitChan := make(chan error)
	go func() {
		qemuWaitChan <- inst.Wait()
	}()

	sigintChan := make(chan os.Signal, 1)
	signal.Notify(sigintChan, os.Interrupt)

	var ip string
	if err = util.Retry(6, 5*time.Second,
		func() error {
			var err error
			ip, err = inst.SSHAddress()
			if err != nil {
				return err
			}
			return nil
		}); err != nil {
		return errors.Wrapf(err, "awaiting ssh address")
	}

	defer func() { fmt.Printf("\n\n") }() // make the console pretty again

	// Start the SSH client
	sc := newSshClient("core", ip, sshKeyPath, sshCommand)
	go sc.controlStartStop()

	ready := false
	statusMsg := "STARTUP"
	lastMsg := ""
	for {
		select {
		// handle ctrl-c. Note: the 'ssh' binary will take over the keyboard and pass
		// it directly to the instance. The intercept of ctrl-c will only happen when
		// ssh is not in the foreground.
		case <-sigintChan:
			inst.Kill()

		// handle console messages. If SSH is not ready, then display a
		// a status message on the console.
		case serialMsg := <-serialChan:
			if !ready {
				displayStatusMsg(statusMsg, serialMsg)
			}
			lastMsg = serialMsg
		// monitor the err channel
		case err := <-errChan:
			if err == platform.ErrInitramfsEmergency {
				return fmt.Errorf("instance failed in initramfs; try rerunning with --devshell-console")
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "errchan: %v", err)
			}

		// monitor the instance state
		case <-qemuWaitChan:
			displayStatusMsg("DONE", "QEMU instance terminated")
			return nil

		// monitor the machine state events from console/serial logs
		case state := <-stateChan:
			if state == guestStateOpenSshStarted {
				ready = true
				sc.controlChan <- sshStart //signal that SSH is ready
				statusMsg = "QEMU guest is ready for SSH"
			} else {
				ready = false
				sc.controlChan <- sshNotReady // signal that SSH should be terminated

				switch state {
				case guestStateInShutdown:
					statusMsg = "QEMU guest is shutting down"
				case guestStateHalted:
					statusMsg = "QEMU guest is halted"
					inst.Kill()
				case guestStateInReboot:
					statusMsg = "QEMU guest initiated reboot"
				case guestStateOpenSshStopped:
					statusMsg = "QEMU openssh is not listening"
				case guestStateSshDisconnected:
					statusMsg = "SSH Client disconnected"
				case guestStateStopHang:
					statusMsg = "QEMU guest waiting for a login service unit to stop"
				case guestStateBooting:
					statusMsg = "QEMU guest is booting"
				}
			}
			displayStatusMsg(fmt.Sprintf("EVENT | %s", statusMsg), lastMsg)

		// monitor the SSH connection
		case err := <-sc.errChan:
			if err == nil {
				sc.controlChan <- sshNotReady
				displayStatusMsg("SESSION", "Clean exit from SSH, terminating instance")
				return nil
			} else if sshCommand != "" {
				sc.controlChan <- sshNotReady
				displayStatusMsg("SESSION", "SSH command exited, terminating instance")
				return err
			}
			if ready {
				sc.controlChan <- sshStart
			}
		}
	}
}

type guestState int

// guestStates are uses to report guests events internal to the host.
const (
	// guestStateOpenSshStarted indicates that the guest has started SSH and
	// is ready for an SSH connection.
	guestStateOpenSshStarted = iota
	// guestStateOpenSshStopped indicates that the SSH server has transitioned
	// from running to stopped.
	guestStateOpenSshStopped
	// guestStateSshDisconnected indicates the SSH client has disconnected
	guestStateSshDisconnected
	// guestStateInShutdown indicates that the guest is shutdown
	guestStateInShutdown
	// guestStateInReboot indicates that the guest has started a reboot
	guestStateInReboot
	// guestStateHalted indicates that the guest has halted or shutdown
	guestStateHalted
	// guestStateBooting indicates that the instance is in early boot
	guestStateBooting
	// guestStateStopHang indicates a job is being slow to shutdown
	guestStateStopHang
)

// checkWriteState checks magical (shutter) strings for state. This is imprecise at best,
// but a whole lot better than nothing.
// TOOO: use the QMP port to query for power-state events
func checkWriteState(msg string, c chan<- guestState) {
	if strings.Contains(msg, "Starting Power-Of") ||
		strings.Contains(msg, "reboot: System halted") ||
		strings.Contains(msg, "ACPI: Preparing to enter system sleep state S5") ||
		strings.Contains(msg, "Starting Halt...") {
		c <- guestStateHalted
	}
	if strings.Contains(msg, "pam_unix(sshd:session): session closed for user core") {
		c <- guestStateSshDisconnected
	}
	if strings.Contains(msg, "The selected entry will be started automatically in 1s.") {
		c <- guestStateBooting
	}
	if strings.Contains(msg, "A stop job is running for User Manager for UID ") ||
		strings.Contains(msg, "Stopped Login Service.") {
		c <- guestStateStopHang
	}

	// Fallback in case the journal is stopped
	if strings.Contains(msg, "Stopped OpenSSH server daemon") ||
		strings.Contains(msg, "Stopped Login Service") {
		c <- guestStateOpenSshStopped
	}
	if strings.Contains(msg, "Reached target Shutdown") {
		c <- guestStateInShutdown
	}
	if strings.Contains(msg, "Starting Reboot...") {
		c <- guestStateInReboot
	}
}

type systemdEventMessage struct {
	Unit      string      `json:"UNIT"`
	MessageID string      `json:"MESSAGE_ID"`
	Message   interface{} `json:"MESSAGE"`
	JobResult string      `json:"JOB_RESULT"`
	JobType   string      `json:"JOB_TYPE"`
}

func (se systemdEventMessage) message() (string, error) {
	if a, ok := se.Message.([]float64); ok {
		r := make([]byte, len(a))
		for i, v := range a {
			r[i] = byte(v)
		}
		return string(r), nil
	} else if s, ok := se.Message.(string); ok {
		return s, nil
	} else {
		return "", fmt.Errorf("Unhandled systemd json message of type %T", se.Message)
	}
}

type systemdMessageCheck struct {
	unit       string
	messageID  string
	message    string
	jobResult  string
	guestState guestState
}

func (sc *systemdMessageCheck) mustMatch(se *systemdEventMessage) bool {
	if sc.unit != se.Unit {
		return false
	}
	if sc.messageID != "" && sc.messageID != se.MessageID {
		return false
	}
	msg, err := se.message()
	// For now let's panic, this is a "shouldn't happen" situation
	if err != nil {
		panic(err)
	}
	if sc.message != "" && sc.message != msg {
		return false
	}
	if sc.jobResult != "" && sc.jobResult != se.JobResult {
		return false
	}
	return true
}

// Systemd monitoring
// src/systemd/sd-messages.h
const (
	// 89:#define SD_MESSAGE_UNIT_STARTED           SD_ID128_MAKE(39,f5,34,79,d3,a0,45,ac,8e,11,78,62,48,23,1f,bf)
	systemdUnitStarted = "39f53479d3a045ac8e11786248231fbf"

	// 93:#define SD_MESSAGE_UNIT_STOPPING          SD_ID128_MAKE(de,5b,42,6a,63,be,47,a7,b6,ac,3e,aa,c8,2e,2f,6f)
	systemdUnitStopping = "de5b426a63be47a7b6ac3eaac82e2f6f"

	// 95:#define SD_MESSAGE_UNIT_STOPPED           SD_ID128_MAKE(9d,1a,aa,27,d6,01,40,bd,96,36,54,38,aa,d2,02,86)
	systemdUnitStopped = "9d1aaa27d60140bd96365438aad20286"
)

// watchJournal watches the virtio journal to monitor for events. This method is NOT 100%
// reliable as the journal may not have started or stopped in time.
func watchJournal(builder *platform.QemuBuilder, conf *conf.Conf, stateChan chan<- guestState, errChan chan<- error) error {
	checkList := []systemdMessageCheck{
		{
			unit:       "sshd.service",
			messageID:  systemdUnitStarted,
			guestState: guestStateOpenSshStarted,
		},
		{
			unit:       "sshd.service",
			messageID:  systemdUnitStopping,
			guestState: guestStateOpenSshStopped,
		},
		{
			unit:       "sshd.service",
			messageID:  systemdUnitStopped,
			guestState: guestStateOpenSshStopped,
		},

		// monitor power events, these are unlikely to be seen unless
		// there is a hang in a unit that's stopping.
		{
			unit:       "systemd-reboot.service",
			messageID:  "7d4958e842da4a758f6c1cdc7b36dcc5",
			guestState: guestStateInReboot,
		},
		{
			unit:       "systemd-halt.service",
			messageID:  "7d4958e842da4a758f6c1cdc7b36dcc5",
			guestState: guestStateHalted,
		},
		{
			unit:       "systemd-poweroff.service",
			messageID:  "7d4958e842da4a758f6c1cdc7b36dcc5",
			guestState: guestStateInShutdown,
		},
	}

	r, err := builder.VirtioJournal(conf, "--system")
	if err != nil {
		return err
	}

	go func() {
		s := bufio.NewScanner(r)
		for s.Scan() {
			msg := s.Text()

			var se systemdEventMessage
			if err := json.Unmarshal([]byte(msg), &se); err != nil {
				errChan <- fmt.Errorf("failed to parse systemd event %s: %w", msg, err)
			}
			for _, rule := range checkList {
				if rule.mustMatch(&se) {
					stateChan <- rule.guestState
				}
			}
		}
	}()

	return nil
}

type sshControlMessage int

// ssh* are control mesages
const (
	// sshStart indicates that the ssh client can/should start.
	sshStart = iota
	// sshNotReady means that the SSH server is not running or has stopped.
	sshNotReady
)

// sshClient represents a single SSH session.
type sshClient struct {
	mu          sync.Mutex
	user        string
	host        string
	port        string
	privKey     string
	cmd         string
	controlChan chan sshControlMessage
	errChan     chan error
	sshCmd      *exec.Cmd
}

// newSshClient creates a new sshClient.
func newSshClient(user, host, privKey, cmd string) *sshClient {
	parts := strings.Split(host, ":")
	host = parts[0]
	port := parts[1]
	if port == "" {
		port = "22"
	}

	return &sshClient{
		mu:          sync.Mutex{},
		user:        user,
		host:        host,
		port:        port,
		privKey:     privKey,
		controlChan: make(chan sshControlMessage),
		errChan:     make(chan error),
		// this could be a []string, but ssh sends it over as a string anyway, so meh...
		cmd: cmd,
	}
}

// start starts the SSH session and returns the error over the errChan.
// While the pure-go SSH client would be nicer, the golang SSH client
// doesn't handle some keyboard keys well (arrows, home, end, etc).
func (sc *sshClient) start() {

	// Ensure that only one SSH command is running
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.sshCmd != nil {
		return
	}

	// On exit clear the SSH command
	defer func() {
		sc.sshCmd = nil
		time.Sleep(1 * time.Second)
	}()

	sshArgs := []string{
		"ssh", "-t",
		"-i", sc.privKey,
		"-o", "StrictHostKeyChecking=no",
		"-o", "CheckHostIP=no",
		"-o", "IdentityFile=/dev/null",
		"-p", sc.port,
		fmt.Sprintf("%s@%s", sc.user, sc.host),
	}
	if sc.cmd != "" {
		sshArgs = append(sshArgs, "--", sc.cmd)
	}
	fmt.Println("") // line break for prettier output
	sshCmd := exec.Command(sshArgs[0], sshArgs[1:]...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	stdErrPipe, err := sshCmd.StderrPipe()
	if err != nil {
		sc.errChan <- err
		return
	}

	go func() {
		scanner := bufio.NewScanner(stdErrPipe)
		for scanner.Scan() {
			msg := scanner.Text()
			if strings.Contains(msg, "Connection to 127.0.0.1 closed") {
				displayStatusMsg("SSH", "connection closed")
			}
		}
	}()

	sc.sshCmd = sshCmd
	sc.errChan <- sc.sshCmd.Run()
}

// kill terminates the SSH session.
func (sc *sshClient) kill() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.sshCmd == nil {
		return
	}
	if sc.sshCmd.ProcessState == nil {
		return
	}
	_ = syscall.Kill(-sc.sshCmd.Process.Pid, syscall.SIGKILL)
	sc.sshCmd = nil
}

// controlStartStop runs an interactive SSH session until its interrrupted.
// Callers should monitor the errChan for updates. controlStartStop ensures that only
// one instance of SSH runs at a time.
func (sc *sshClient) controlStartStop() {
	for {
		msg := <-sc.controlChan
		if msg == sshNotReady {
			go sc.kill()
		}
		if msg == sshStart {
			go sc.start()
		}
	}
}
