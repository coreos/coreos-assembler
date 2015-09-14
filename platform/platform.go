// Copyright 2014 CoreOS, Inc.
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
	"fmt"
	"io"
	"os"
	"time"

	"path/filepath"

	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/coreos/mantle/util"
)

const (
	sshRetries = 7
	sshTimeout = 1 * time.Second
)

type Machine interface {
	ID() string
	IP() string
	PrivateIP() string
	SSHSession() (*ssh.Session, error)
	SSH(cmd string) ([]byte, error)
	Destroy() error
	StartJournal() error
}

type Cluster interface {
	NewCommand(name string, arg ...string) util.Cmd
	NewMachine(config string) (Machine, error)
	Machines() []Machine
	// Points to an embedded etcd for QEMU, not sure what this
	// is going to look like for other platforms yet.
	EtcdEndpoint() string
	GetDiscoveryURL(size int) (string, error)
	Destroy() error
}

// TestCluster embedds a Cluster to provide platform independant helper
// methods.
type TestCluster struct {
	Name        string
	NativeFuncs []string
	Options     map[string]string
	Cluster
}

// run a registered NativeFunc on a remote machine
func (t *TestCluster) RunNative(funcName string, m Machine) error {
	// scp and execute kolet on remote machine
	ssh, err := m.SSHSession()
	if err != nil {
		return fmt.Errorf("kolet SSH session: %v", err)
	}
	b, err := ssh.CombinedOutput(fmt.Sprintf("./kolet run %q %q", t.Name, funcName))
	if err != nil {
		return fmt.Errorf("%s", b) // return function std output, not the exit status
	}
	return nil
}

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
		if err := transfer(in, m, localPath); err != nil {
			return err
		}
	}
	return nil
}

func transfer(in io.Reader, m Machine, localPath string) error {
	session, err := m.SSHSession()
	if err != nil {
		return fmt.Errorf("Error establishing ssh session: %v", err)
	}
	defer session.Close()

	session.Stdin = in

	// write file to fs from stdin
	_, filename := filepath.Split(localPath)
	err = session.Run(fmt.Sprintf("install -m 0755 /dev/stdin ./%s", filename))
	if err != nil {
		return err
	}

	return nil
}
