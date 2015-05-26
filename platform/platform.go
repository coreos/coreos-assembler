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

	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/coreos/mantle/util"
)

type Machine interface {
	ID() string
	IP() string
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
	Name string
	Cluster
}

// run a registered NativeFunc on a remote machine
func (t *TestCluster) RunNative(funcName string, m Machine) error {
	// scp and execute kolet on remote machine
	b, err := m.SSH(fmt.Sprintf("./kolet run %q %q", t.Name, funcName))
	if err != nil {
		return fmt.Errorf("%v: %s", err, b)
	}
	return nil
}
