// Copyright 2016 CoreOS, Inc.
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

package cluster

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kballard/go-shellquote"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/cluster")
)

// TestCluster embedds a Cluster to provide platform independant helper
// methods.
type TestCluster struct {
	*harness.H
	platform.Cluster
	NativeFuncs []string

	// If set to true and a sub-test fails all future sub-tests will be skipped
	FailFast   bool
	hasFailure bool
}

// Run runs f as a subtest and reports whether f succeeded.
func (t *TestCluster) Run(name string, f func(c TestCluster)) bool {
	if t.FailFast && t.hasFailure {
		return t.H.Run(name, func(h *harness.H) {
			func(c TestCluster) {
				c.Skip("A previous test has already failed")
			}(TestCluster{H: h, Cluster: t.Cluster})
		})
	}
	t.hasFailure = !t.H.Run(name, func(h *harness.H) {
		f(TestCluster{H: h, Cluster: t.Cluster})
	})
	return !t.hasFailure

}

// Same as Run, but logs a message in the machines' journal logs before and
// after running the function.
func (t *TestCluster) RunLogged(name string, f func(c TestCluster)) bool {
	for _, m := range t.Machines() {
		t.JournalLog(m, "=== RUN: %s/%s ===", t.H.Name(), name)
		defer t.JournalLog(m, "=== DONE: %s/%s ===", t.H.Name(), name)
	}
	return t.Run(name, f)
}

// RunNative runs a registered NativeFunc on a remote machine
func (t *TestCluster) RunNative(funcName string, m platform.Machine) bool {
	command := fmt.Sprintf("./kolet run %q %q", t.H.Name(), funcName)
	return t.Run(funcName, func(c TestCluster) {
		client, err := m.SSHClient()
		if err != nil {
			c.Fatalf("kolet SSH client: %v", err)
		}
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			c.Fatalf("kolet SSH session: %v", err)
		}
		defer session.Close()

		b, err := session.CombinedOutput(command)
		b = bytes.TrimSpace(b)
		if len(b) > 0 {
			t.Logf("kolet:\n%s", b)
		}
		if err != nil {
			c.Errorf("kolet: %v", err)
		}
	})
}

// ListNativeFunctions returns a slice of function names that can be executed
// directly on machines in the cluster.
func (t *TestCluster) ListNativeFunctions() []string {
	return t.NativeFuncs
}

// DropLabeledFile places file from localPath to ~/ on every machine in
// cluster, potentially with a custom SELinux label.
func DropLabeledFile(machines []platform.Machine, localPath, selabel string) error {
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()

	for _, m := range machines {
		if _, err := in.Seek(0, 0); err != nil {
			return err
		}
		// write to a separate path first, then rename to its final location so
		// that anything watching the path can only get a complete file
		base := filepath.Base(localPath)
		partial := base + ".partial"
		if err := platform.InstallFile(in, m, partial); err != nil {
			return err
		}
		if selabel != "" {
			if out, stderr, err := m.SSH(fmt.Sprintf("sudo chcon -t %s %s.partial", selabel, base)); err != nil {
				return errors.Wrapf(err, "running chcon on %s.partial: %s: %s", base, out, stderr)
			}
		}
		if out, stderr, err := m.SSH(fmt.Sprintf("mv %[1]s.partial %[1]s", base)); err != nil {
			return errors.Wrapf(err, "running mv %[1]s.partial %[1]s: %s: %s", base, out, stderr)
		}
	}
	return nil
}

// DropFile places file from localPath to ~/ on every machine in cluster
func DropFile(machines []platform.Machine, localPath string) error {
	return DropLabeledFile(machines, localPath, "")
}

// SSH runs a ssh command on the given machine in the cluster. It differs from
// Machine.SSH in that stderr is written to the test's output as a 'Log' line.
// This ensures the output will be correctly accumulated under the correct
// test.
func (t *TestCluster) SSH(m platform.Machine, cmd string) ([]byte, error) {
	var stdout, stderr []byte
	var err error
	f := func() {
		stdout, stderr, err = m.SSH(cmd)
	}

	errMsg := fmt.Sprintf("ssh: %s", cmd)
	// If f does not before the test timeout, the RunWithExecTimeoutCheck
	// will end this goroutine and mark the test as failed
	t.H.RunWithExecTimeoutCheck(f, errMsg)
	if len(stderr) > 0 {
		for _, line := range strings.Split(string(stderr), "\n") {
			t.Log(line)
		}
	}
	return stdout, err
}

func (t *TestCluster) SSHf(m platform.Machine, f string, args ...interface{}) ([]byte, error) {
	return t.SSH(m, fmt.Sprintf(f, args...))
}

// MustSSH runs a ssh command on the given machine in the cluster, writes
// its stderr to the test's output as a 'Log' line, fails the test if the
// command is unsuccessful, and returns the command's stdout.
func (t *TestCluster) MustSSH(m platform.Machine, cmd string) []byte {
	out, err := t.SSH(m, cmd)
	if err != nil {
		if t.SSHOnTestFailure() {
			plog.Errorf("dropping to shell: %q failed: output %s, status %v", cmd, out, err)
			if err := platform.Manhole(m); err != nil {
				plog.Error(err)
			}
		}
		t.Fatalf("%q failed: output %s, status %v", cmd, out, err)
	}
	return out
}

func (t *TestCluster) MustSSHf(m platform.Machine, f string, args ...interface{}) []byte {
	return t.MustSSH(m, fmt.Sprintf(f, args...))
}

func (t *TestCluster) JournalLog(m platform.Machine, f string, args ...interface{}) []byte {
	return t.MustSSH(m, fmt.Sprintf("logger --tag kola '%s'", fmt.Sprintf(f, args...)))
}

// RunCmdSync synchronously exectues a command, logging the output to the journal
func (t *TestCluster) RunCmdSync(m platform.Machine, cmd string) {
	t.LogJournal(m, "+ "+cmd)
	// Pipe stdout/stderr into the systemd journal
	_, err := t.SSH(m, "set -o pipefail; "+cmd+" |& logger -t kola")
	if err != nil {
		if t.SSHOnTestFailure() {
			plog.Errorf("dropping to shell: %q failed: %v", cmd, err)
			if err := platform.Manhole(m); err != nil {
				plog.Error(err)
			}
		}
		journalout, journalErr := t.SSH(m, "journalctl -q --no-pager -n 20 -o cat -t kola")
		if journalErr != nil {
			journalout = []byte(fmt.Sprintf("<failed to gather journal for kola: %v", journalErr))
		}
		t.Fatalf("%q failed: %v; logs from journalctl -t kola:\n%s", cmd, err, string(journalout))
	}
}

// RunCmdSyncf is like RunCmdSync but accepts formatted arguments.
func (t *TestCluster) RunCmdSyncf(m platform.Machine, f string, args ...interface{}) {
	t.RunCmdSync(m, fmt.Sprintf(f, args...))
}

// AssertCmdOutputContains runs cmd via SSH and panics if stdout does not contain expected
func (t *TestCluster) AssertCmdOutputContains(m platform.Machine, cmd string, expected string) {
	t.LogJournal(m, "+ "+cmd)
	outputBuf := t.MustSSH(m, cmd)
	output := string(outputBuf)
	if !strings.Contains(output, expected) {
		t.Fatalf("cmd %s did not output %s", cmd, expected)
	}
}

// AssertCmdOutputContains runs cmd via SSH and panics if stdout does not contain expected
func (t *TestCluster) AssertCmdOutputMatches(m platform.Machine, cmd string, expected *regexp.Regexp) {
	t.LogJournal(m, "+ "+cmd)
	output := t.MustSSH(m, cmd)
	if !expected.Match(output) {
		t.Fatalf("cmd %s output did not match regexp %s: %s", cmd, expected, string(output))
	}
}

// Synchronously write a log message from the syslog identifier `kola` into the target
// machine's journal (via ssh) as well as at a debug log level to the current process.
// This is useful for debugging test failures, as we always capture the target
// system journal.
func (t *TestCluster) LogJournal(m platform.Machine, msg string) {
	t.MustSSH(m, fmt.Sprintf("logger -t kola %s", shellquote.Join(msg)))
}

func (t *TestCluster) LogJournalf(m platform.Machine, f string, args ...interface{}) {
	t.LogJournal(m, fmt.Sprintf(f, args...))
}
