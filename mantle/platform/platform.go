// Copyright 2017 CoreOS, Inc.
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform")
)

// Name is a unique identifier for a platform.
type Name string

// Machine represents a Container Linux instance.
type Machine interface {
	// ID returns the plaform-specific machine identifier.
	ID() string

	// IgnitionError returns an error if the machine failed in Ignition
	IgnitionError() error

	// IP returns the machine's public IP.
	IP() string

	// PrivateIP returns the machine's private IP.
	PrivateIP() string

	// RuntimeConf returns the cluster's runtime configuration.
	RuntimeConf() RuntimeConfig

	// SSHClient establishes a new SSH connection to the machine.
	SSHClient() (*ssh.Client, error)

	// PasswordSSHClient establishes a new SSH connection using the provided credentials.
	PasswordSSHClient(user string, password string) (*ssh.Client, error)

	// SSH runs a single command over a new SSH connection.
	SSH(cmd string) ([]byte, []byte, error)

	// Start sets up the journal and performs sanity checks via platform.StartMachine().
	Start() error

	// Reboot restarts the machine and waits for it to come back.
	Reboot() error

	// WaitForReboot waits for the machine to restart and waits for it to come back.
	WaitForReboot(time.Duration, string) error

	// Destroy terminates the machine and frees associated resources. It should log
	// any failures; since they are not actionable, it does not return an error.
	Destroy()

	// ConsoleOutput returns the machine's console output if available,
	// or an empty string.  Only expected to be valid after Destroy().
	ConsoleOutput() string

	// JournalOutput returns the machine's journal output if available,
	// or an empty string.  Only expected to be valid after Destroy().
	JournalOutput() string
}

// Cluster represents a cluster of machines within a single Flight.
type Cluster interface {
	// Platform returns the name of the platform.
	Platform() Name

	// Name returns a unique name for the Cluster.
	Name() string

	// NewMachine creates a new CoreOS machine.
	NewMachine(userdata *conf.UserData) (Machine, error)

	// NewMachineWithOptions creates a new CoreOS machine as defined by the given options.
	NewMachineWithOptions(userdata *conf.UserData, options MachineOptions) (Machine, error)

	// Machines returns a slice of the active machines in the Cluster.
	Machines() []Machine

	// Destroy terminates each machine in the cluster and frees any other
	// associated resources. It should log any failures; since they are not
	// actionable, it does not return an error
	Destroy()

	// ConsoleOutput returns a map of console output from destroyed
	// cluster machines.
	ConsoleOutput() map[string]string

	// JournalOutput returns a map of journal output from destroyed
	// cluster machines.
	JournalOutput() map[string]string

	// Distribution returns the Distribution
	Distribution() string

	// SSHOnTestFailure returns whether the cluster should Manhole into
	// a machine when a MustSSH call fails
	SSHOnTestFailure() bool
}

// Flight represents a group of Clusters within a single platform.
type Flight interface {
	// NewCluster creates a new Cluster.
	NewCluster(rconf *RuntimeConfig) (Cluster, error)

	// Name returns a unique name for the Flight.
	Name() string

	// Platform returns the name of the platform.
	Platform() Name

	// Clusters returns a slice of the active Clusters.
	Clusters() []Cluster

	// ConfigTooLarge returns true iff the config is too
	// large for the platform
	ConfigTooLarge(ud conf.UserData) bool

	// Destroy terminates each cluster and frees any other associated
	// resources.  It should log any failures; since they are not
	// actionable, it does not return an error.
	Destroy()
}

type MachineOptions struct {
	MultiPathDisk             bool
	AdditionalDisks           []string
	MinMemory                 int
	MinDiskSize               int
	AdditionalNics            int
	AppendKernelArgs          string
	AppendFirstbootKernelArgs string
	SkipStartMachine          bool // Skip platform.StartMachine on machine bringup
}

// SystemdDropin is a userdata type agnostic struct representing a systemd dropin
type SystemdDropin struct {
	Unit     string
	Name     string
	Contents string
}

// Options contains the base options for all clusters.
type Options struct {
	BaseName       string
	Distribution   string
	SystemdDropins []SystemdDropin
	Stream         string

	CosaWorkdir   string
	CosaBuildId   string
	CosaBuildArch string

	UseWarnExitCode77 bool

	AppendButane   string
	AppendIgnition string

	// OSContainer is an image pull spec that can be given to the pivot service
	// in RHCOS machines to perform machine content upgrades.
	// When specified additional files & units will be automatically generated
	// inside of RenderUserData
	OSContainer string

	SSHOnTestFailure bool

	ExtendTimeoutPercent uint
}

// RuntimeConfig contains cluster-specific configuration.
type RuntimeConfig struct {
	OutputDir string

	NoSSHKeyInUserData bool                // don't inject SSH key into Ignition/cloud-config
	NoSSHKeyInMetadata bool                // don't add SSH key to platform metadata
	NoInstanceCreds    bool                // don't grant credentials (AWS instance profile, GCP service account) to the instance
	AllowFailedUnits   bool                // don't fail CheckMachine if a systemd unit has failed
	WarningsAction     conf.WarningsAction // what to do on Ignition or Butane validation warnings

	// InternetAccess is true if the cluster should be Internet connected
	InternetAccess bool
	EarlyRelease   func()

	// whether a Manhole into a machine should be created on detected failure
	SSHOnTestFailure bool
}

// Wrap a StdoutPipe as a io.ReadCloser
type sshPipe struct {
	s   *ssh.Session
	c   *ssh.Client
	err *bytes.Buffer
	io.Reader
}

func (p *sshPipe) Close() error {
	if err := p.s.Wait(); err != nil {
		return fmt.Errorf("%s: %s", err, p.err)
	}
	if err := p.s.Close(); err != nil {
		return err
	}
	return p.c.Close()
}

// Copy a file between two machines in a cluster.
func TransferFile(src Machine, srcPath string, dst Machine, dstPath string) error {
	srcPipe, err := ReadFile(src, srcPath)
	if err != nil {
		return err
	}
	defer srcPipe.Close()

	if err := InstallFile(srcPipe, dst, dstPath); err != nil {
		return err
	}
	return nil
}

// ReadFile returns a io.ReadCloser that streams the requested file. The
// caller should close the reader when finished.
func ReadFile(m Machine, path string) (io.ReadCloser, error) {
	client, err := m.SSHClient()
	if err != nil {
		return nil, errors.Wrapf(err, "failed creating SSH client")
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, errors.Wrapf(err, "failed creating SSH session")
	}

	// connect session stdout
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	// collect stderr
	errBuf := bytes.NewBuffer(nil)
	session.Stderr = errBuf

	// stream file to stdout
	err = session.Start(fmt.Sprintf("sudo cat %s", path))
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	// pass stdoutPipe as a io.ReadCloser that cleans up the ssh session
	// on when closed.
	return &sshPipe{session, client, errBuf, stdoutPipe}, nil
}

// InstallFile copies data from in to the path to on m.
func InstallFile(in io.Reader, m Machine, to string) error {
	dir := filepath.Dir(to)
	out, stderr, err := m.SSH(fmt.Sprintf("sudo mkdir -p %s", dir))
	if err != nil {
		return fmt.Errorf("failed creating directory %s: %q: %s: %s", dir, out, stderr, err)
	}

	client, err := m.SSHClient()
	if err != nil {
		return errors.Wrapf(err, "failed creating SSH client")
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return errors.Wrapf(err, "failed creating SSH session")
	}

	defer session.Close()

	// write file to fs from stdin
	session.Stdin = in
	out, err = session.CombinedOutput(fmt.Sprintf("sudo install -m 0755 /dev/stdin %s", to))
	if err != nil {
		return fmt.Errorf("failed executing install: %q: %v", out, err)
	}

	return nil
}

// CopyDirToMachine synchronizes the local contents of inputdir to the
// remote destdir.
func CopyDirToMachine(inputdir string, m Machine, destdir string) error {
	out, stderr, err := m.SSH(fmt.Sprintf("sudo mkdir -p %s", shellquote.Join(destdir)))
	if err != nil {
		return errors.Wrapf(err, "failed creating directory %s: %q: %s", destdir, out, stderr)
	}

	client, err := m.SSHClient()
	if err != nil {
		return errors.Wrapf(err, "failed creating SSH client")
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return errors.Wrapf(err, "failed creating SSH session")
	}

	defer session.Close()

	// Use compression level 1 for speed
	compressArgv := []string{"-c", "tar chf - . | gzip -1"}

	clientCmd := exec.Command("/bin/sh", compressArgv...)
	clientCmd.Dir = inputdir
	stdout, err := clientCmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	err = clientCmd.Start()
	if err != nil {
		return err
	}

	session.Stdin = stdout
	out, err = session.CombinedOutput(fmt.Sprintf("sudo tar -xz -C %s -f -", shellquote.Join(destdir)))
	if err != nil {
		return errors.Wrapf(err, "executing remote untar: %q", out)
	}

	err = clientCmd.Wait()
	if err != nil {
		return errors.Wrapf(err, "local tar")
	}

	return nil
}

// NewMachines spawns n instances in cluster c, with
// each instance passed the same userdata.
func NewMachines(c Cluster, userdata *conf.UserData, n int, options MachineOptions) ([]Machine, error) {
	var wg sync.WaitGroup

	mchan := make(chan Machine, n)
	errchan := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := c.NewMachineWithOptions(userdata, options)
			if err != nil {
				errchan <- err
			}
			if m != nil {
				mchan <- m
			}
		}()
	}

	wg.Wait()
	close(mchan)
	close(errchan)

	machs := []Machine{}

	for m := range mchan {
		machs = append(machs, m)
	}

	if firsterr, ok := <-errchan; ok {
		for _, m := range machs {
			m.Destroy()
		}
		return nil, firsterr
	}

	return machs, nil
}

// checkSystemdUnitFailures ensures that no system unit is in a failed state.
func checkSystemdUnitFailures(output string) error {
	if len(output) > 0 {
		return fmt.Errorf("some systemd units failed: %s", output)
	}
	return nil
}

// checkSystemdUnitStuck ensures that no system unit stuck in activating state.
// https://github.com/coreos/coreos-assembler/issues/2798
// See https://bugzilla.redhat.com/show_bug.cgi?id=2072050
func checkSystemdUnitStuck(output string, m Machine) error {
	if len(output) == 0 {
		return nil
	}
	var NRestarts int
	for _, unit := range strings.Split(output, "\n") {
		out, stderr, err := m.SSH(fmt.Sprintf("systemctl show -p NRestarts --value %s", unit))
		if err != nil {
			return fmt.Errorf("failed to query systemd unit NRestarts: %s: %v: %s", out, err, stderr)
		}
		NRestarts, _ = strconv.Atoi(string(out))
		if NRestarts >= 2 {
			return fmt.Errorf("systemd units %s has %v restarts", unit, NRestarts)
		}
	}
	return nil
}

// CheckMachine tests a machine for various error conditions such as ssh
// being available and no systemd units failing at the time ssh is reachable.
// It also ensures the remote system is running Container Linux by CoreOS or
// Red Hat CoreOS.
func CheckMachine(ctx context.Context, m Machine) error {
	// ensure ssh works and the system is ready
	sshChecker := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// By design, `systemctl is-system-running` returns nonzero codes based on its state.
		// We want to explicitly accept some nonzero states and test instead by the string so
		// add `|| :`.
		out, stderr, err := m.SSH("systemctl is-system-running || :")
		if !bytes.Contains([]byte("initializing starting running stopping"), out) {
			return nil // stop retrying if the system went haywire
		}
		if err != nil {
			return fmt.Errorf("could not check if machine is running: %s: %v: %s", out, err, stderr)
		}
		return nil
	}

	if err := util.RetryUntilTimeout(10*time.Minute, 10*time.Second, sshChecker); err != nil {
		return errors.Wrapf(err, "ssh unreachable")
	}

	out, stderr, err := m.SSH(`. /etc/os-release && echo "$ID"`)
	if err != nil {
		return fmt.Errorf("no /etc/os-release file: %v: %s", err, stderr)
	}
	osReleaseID := string(out)

	out, stderr, err = m.SSH(`. /etc/os-release && echo "$VARIANT_ID"`)
	if err != nil {
		return fmt.Errorf("no /etc/os-release file: %v: %s", err, stderr)
	}
	osReleaseVariantID := string(out)

	// Basic check to ensure that we're talking to a supported system
	if osReleaseVariantID != "coreos" {
		return fmt.Errorf("not a supported instance: %s %s", osReleaseID, osReleaseVariantID)
	}

	// check systemd version on host to see if we can use `busctl --json=short`
	var systemdVer int
	var systemdCmd, failedUnitsCmd, activatingUnitsCmd string
	var systemdFailures bool
	minSystemdVer := 240
	out, stderr, err = m.SSH("rpm -q --queryformat='%{VERSION}\n' systemd")
	if err != nil {
		return fmt.Errorf("failed to query systemd RPM for version: %s: %v: %s", out, err, stderr)
	}
	// Fedora can use XXX.Y as a version string, so just use the major version
	systemdVer, _ = strconv.Atoi(string(out[0:3]))

	if systemdVer >= minSystemdVer {
		systemdCmd = "busctl --json=short call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager ListUnitsFiltered as 2 state status | jq -r '.data[][][0]'"
	} else {
		systemdCmd = "systemctl --no-legend --state status list-units | awk '{print $1}'"
	}
	failedUnitsCmd = strings.Replace(systemdCmd, "status", "failed", -1)
	activatingUnitsCmd = strings.Replace(systemdCmd, "status", "activating", -1)

	// Ensure no systemd units failed during boot
	out, stderr, err = m.SSH(failedUnitsCmd)
	if err != nil {
		return fmt.Errorf("failed to query systemd for failed units: %s: %v: %s", out, err, stderr)
	}
	err = checkSystemdUnitFailures(string(out))
	if err != nil {
		plog.Error(err)
		systemdFailures = true
	}

	// Ensure no systemd units stuck in activating state
	out, stderr, err = m.SSH(activatingUnitsCmd)
	if err != nil {
		return fmt.Errorf("failed to query systemd for activating units: %s: %v: %s", out, err, stderr)
	}
	err = checkSystemdUnitStuck(string(out), m)
	if err != nil {
		plog.Error(err)
		systemdFailures = true
	}

	if systemdFailures && !m.RuntimeConf().AllowFailedUnits {
		if m.RuntimeConf().SSHOnTestFailure {
			plog.Error("dropping to shell: detected failed or stuck systemd units")
			if err := Manhole(m); err != nil {
				plog.Errorf("failed to get terminal via ssh: %v", err)
			}
		}
		return fmt.Errorf("detected failed or stuck systemd units")
	}

	return ctx.Err()
}

type machineInfo struct {
	Id        string `json:"id"`
	PublicIp  string `json:"public_ip"`
	OutputDir string `json:"output_dir"`
}

func WriteJSONInfo(m Machine, w io.Writer) error {
	info := machineInfo{
		Id:        m.ID(),
		PublicIp:  m.IP(),
		OutputDir: filepath.Join(m.RuntimeConf().OutputDir, m.ID()),
	}
	e := json.NewEncoder(w)
	// disable pretty printing, so we emit newline-delimited streaming JSON objects
	e.SetIndent("", "")
	return e.Encode(info)
}
