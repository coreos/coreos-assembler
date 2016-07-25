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
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/kola/skip"
	"github.com/coreos/mantle/platform"
)

// TestCluster embedds a Cluster to provide platform independant helper
// methods.
type TestCluster struct {
	Name        string
	NativeFuncs []string
	Options     map[string]string
	platform.Cluster
}

// RunNative runs a registered NativeFunc on a remote machine
func (t *TestCluster) RunNative(funcName string, m platform.Machine) error {
	// scp and execute kolet on remote machine
	client, err := m.SSHClient()
	if err != nil {
		return fmt.Errorf("kolet SSH client: %v", err)
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("kolet SSH session: %v", err)
	}

	defer session.Close()

	b, err := session.CombinedOutput(fmt.Sprintf("./kolet run %q %q", t.Name, funcName))
	if err != nil {
		return fmt.Errorf("%s", b) // return function std output, not the exit status
	}
	return nil
}

// ListNativeFunctions returns a slice of function names that can be executed
// directly on machines in the cluster.
func (t *TestCluster) ListNativeFunctions() []string {
	return t.NativeFuncs
}

// DropFile places file from localPath to ~/ on every machine in cluster
func (t *TestCluster) DropFile(localPath string) error {
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()

	for _, m := range t.Machines() {
		if _, err := in.Seek(0, 0); err != nil {
			return err
		}
		if err := platform.InstallFile(in, m, filepath.Base(localPath)); err != nil {
			return err
		}
	}
	return nil
}

// Error, Errorf, Skip, and Skipf partially implement testing.TB.

func (t *TestCluster) err(e error) {
	panic(e)
}

func (t *TestCluster) Error(e error) {
	t.err(e)
}

func (t *TestCluster) Errorf(format string, args ...interface{}) {
	t.err(fmt.Errorf(format, args...))
}
func (t *TestCluster) skip(why string) {
	panic(skip.Skip(why))
}

func (t *TestCluster) Skip(args ...interface{}) {
	t.skip(fmt.Sprint(args...))
}

func (t *TestCluster) Skipf(format string, args ...interface{}) {
	t.skip(fmt.Sprintf(format, args...))
}
