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

package packet

import (
	"context"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/packethost/packngo"
)

type machine struct {
	cluster   *cluster
	device    *packngo.Device
	journal   *platform.Journal
	publicIP  string
	privateIP string
}

func (pm *machine) ID() string {
	return pm.device.ID
}

func (pm *machine) IP() string {
	return pm.publicIP
}

func (pm *machine) PrivateIP() string {
	return pm.privateIP
}

func (pm *machine) SSHClient() (*ssh.Client, error) {
	return pm.cluster.SSHClient(pm.IP())
}

func (pm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return pm.cluster.PasswordSSHClient(pm.IP(), user, password)
}

func (pm *machine) SSH(cmd string) ([]byte, error) {
	return pm.cluster.SSH(pm, cmd)
}

func (m *machine) Reboot() error {
	if err := platform.StartReboot(m); err != nil {
		return err
	}
	if err := m.journal.Start(context.TODO(), m); err != nil {
		return err
	}
	if err := platform.CheckMachine(m); err != nil {
		return err
	}
	if err := platform.EnableSelinux(m); err != nil {
		return err
	}
	return nil
}

func (pm *machine) Destroy() error {
	if err := pm.cluster.api.DeleteDevice(pm.ID()); err != nil {
		return err
	}

	if pm.journal != nil {
		if err := pm.journal.Destroy(); err != nil {
			return err
		}
	}

	pm.cluster.DelMach(pm)
	return nil
}

func (pm *machine) ConsoleOutput() string {
	// TODO(bgilbert)
	return ""
}
