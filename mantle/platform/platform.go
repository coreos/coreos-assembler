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
	"strings"
	"sync"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform")
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

	NoTestExitError bool

	// OSContainer is an image pull spec that can be given to the pivot service
	// in RHCOS machines to perform machine content upgrades.
	// When specified additional files & units will be automatically generated
	// inside of RenderUserData
	OSContainer string

	SSHOnTestFailure bool
}

// RuntimeConfig contains cluster-specific configuration.
type RuntimeConfig struct {
	OutputDir string

	NoSSHKeyInUserData bool                // don't inject SSH key into Ignition/cloud-config
	NoSSHKeyInMetadata bool                // don't add SSH key to platform metadata
	AllowFailedUnits   bool                // don't fail CheckMachine if a systemd unit has failed
	WarningsAction     conf.WarningsAction // what to do on Ignition or Butane validation warnings

	// InternetAccess is true if the cluster should be Internet connected
	InternetAccess bool
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

// checkSystemdUnitFailures ensures that no system unit is in a failed state,
// temporarily ignoring some non-fatal flakes on RHCOS.
// See: https://bugzilla.redhat.com/show_bug.cgi?id=1914362
func checkSystemdUnitFailures(output string, distribution string) error {
	if len(output) == 0 {
		return nil
	}

	var ignoredUnits []string
	if distribution == "rhcos" {
		ignoredUnits = append(ignoredUnits, "user@1000.service")
		ignoredUnits = append(ignoredUnits, "user-runtime-dir@1000.service")
	}

	var failedUnits []string
	for _, unit := range strings.Split(output, "\n") {
		// Filter ignored units
		ignored := false
		for _, i := range ignoredUnits {
			if unit == i {
				ignored = true
				break
			}
		}
		if !ignored {
			failedUnits = append(failedUnits, unit)
		}
	}
	if len(failedUnits) > 0 {
		return fmt.Errorf("some systemd units failed:\n%s", output)
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
		out, stderr, err := m.SSH("systemctl is-system-running")
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

	out, stderr, err := m.SSH(`. /etc/os-release && echo "$ID-$VARIANT_ID"`)
	if err != nil {
		return fmt.Errorf("no /etc/os-release file: %v: %s", err, stderr)
	}

	// ensure we're talking to a supported system
	var distribution string
	switch string(out) {
	case `fedora-coreos`:
		distribution = "fcos"
	case `centos-coreos`, `rhcos-`:
		distribution = "rhcos"
	default:
		return fmt.Errorf("not a supported instance: %v", string(out))
	}

	if !m.RuntimeConf().AllowFailedUnits {
		// Ensure no systemd units failed during boot
		out, stderr, err = m.SSH("busctl --json=short call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager ListUnitsFiltered as 2 state failed | jq -r '.data[][][0]'")
		if err != nil {
			return fmt.Errorf("failed to query systemd for failed units: %s: %v: %s", out, err, stderr)
		}
		err = checkSystemdUnitFailures(string(out), distribution)
		if err != nil {
			return err
		}
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
