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

package do

import (
	"context"
	"strconv"
	"time"

	"github.com/digitalocean/godo"
	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
)

type machine struct {
	cluster   *cluster
	droplet   *godo.Droplet
	journal   *platform.Journal
	publicIP  string
	privateIP string
}

func (dm *machine) ID() string {
	return strconv.Itoa(dm.droplet.ID)
}

func (dm *machine) IP() string {
	return dm.publicIP
}

func (dm *machine) PrivateIP() string {
	return dm.privateIP
}

func (dm *machine) RuntimeConf() platform.RuntimeConfig {
	return dm.cluster.RuntimeConf()
}

func (dm *machine) SSHClient() (*ssh.Client, error) {
	return dm.cluster.SSHClient(dm.IP())
}

func (dm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return dm.cluster.PasswordSSHClient(dm.IP(), user, password)
}

func (dm *machine) SSH(cmd string) ([]byte, []byte, error) {
	return dm.cluster.SSH(dm, cmd)
}

func (dm *machine) IgnitionError() error {
	return nil
}

func (dm *machine) Start() error {
	return platform.StartMachine(dm, dm.journal)
}

func (dm *machine) Reboot() error {
	return platform.RebootMachine(dm, dm.journal)
}

func (dm *machine) WaitForReboot(timeout time.Duration, oldBootId string) error {
	return platform.WaitForMachineReboot(dm, dm.journal, timeout, oldBootId)
}

func (dm *machine) Destroy() {
	if err := dm.cluster.flight.api.DeleteDroplet(context.TODO(), dm.droplet.ID); err != nil {
		plog.Errorf("Error deleting droplet %v: %v", dm.droplet.ID, err)
	}

	if dm.journal != nil {
		dm.journal.Destroy()
	}

	dm.cluster.DelMach(dm)
}

func (dm *machine) ConsoleOutput() string {
	// DigitalOcean provides no API for retrieving ConsoleOutput
	return ""
}

func (dm *machine) JournalOutput() string {
	if dm.journal == nil {
		return ""
	}

	data, err := dm.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for droplet %v: %v", dm.droplet.ID, err)
	}
	return string(data)
}
