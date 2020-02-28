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
	"strings"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/pkg/capnslog"
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

// DropFile places file from localPath to ~/ on every machine in cluster
func DropFile(machines []platform.Machine, localPath string) error {
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()

	for _, m := range machines {
		if _, err := in.Seek(0, 0); err != nil {
			return err
		}
		if err := platform.InstallFile(in, m, filepath.Base(localPath)); err != nil {
			return err
		}
	}
	return nil
}

// SSH runs a ssh command on the given machine in the cluster. It differs from
// Machine.SSH in that stderr is written to the test's output as a 'Log' line.
// This ensures the output will be correctly accumulated under the correct
// test.
func (t *TestCluster) SSH(m platform.Machine, cmd string) ([]byte, error) {
	stdout, stderr, err := m.SSH(cmd)

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
			platform.Manhole(m)
		}
		t.Fatalf("%q failed: output %s, status %v", cmd, out, err)
	}
	return out
}

func (t *TestCluster) MustSSHf(m platform.Machine, f string, args ...interface{}) []byte {
	return t.MustSSH(m, fmt.Sprintf(f, args...))
}
