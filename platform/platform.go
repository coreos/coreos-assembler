/*
   Copyright 2014 CoreOS, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package platform

import (
	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/util"
)

type Machine interface {
	ID() string
	IP() string
	SSHSession() (*ssh.Session, error)
	SSH(cmd string) ([]byte, error)
	Destroy() error
}

type Cluster interface {
	NewCommand(name string, arg ...string) util.Cmd
	NewMachine(config string) (Machine, error)
	Machines() []Machine
	// Points to an embedded etcd for QEMU, not sure what this
	// is going to look like for other platforms yet.
	EtcdEndpoint() string
	Destroy() error
}
