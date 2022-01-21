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
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/packethost/packngo"
)

type machine struct {
	cluster   *cluster
	device    *packngo.Device
	journal   *platform.Journal
	console   *console
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

func (pm *machine) RuntimeConf() platform.RuntimeConfig {
	return pm.cluster.RuntimeConf()
}

func (pm *machine) SSHClient() (*ssh.Client, error) {
	return pm.cluster.SSHClient(pm.IP())
}

func (pm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return pm.cluster.PasswordSSHClient(pm.IP(), user, password)
}

func (pm *machine) SSH(cmd string) ([]byte, []byte, error) {
	return pm.cluster.SSH(pm, cmd)
}

func (pm *machine) IgnitionError() error {
	return nil
}

func (pm *machine) Start() error {
	return platform.StartMachine(pm, pm.journal)
}

func (pm *machine) Reboot() error {
	return platform.RebootMachine(pm, pm.journal)
}

func (pm *machine) WaitForReboot(timeout time.Duration, oldBootId string) error {
	return platform.WaitForMachineReboot(pm, pm.journal, timeout, oldBootId)
}

func (pm *machine) Destroy() {
	if err := pm.cluster.flight.api.DeleteDevice(pm.ID()); err != nil {
		plog.Errorf("Error terminating device %v: %v", pm.ID(), err)
	}

	if pm.journal != nil {
		pm.journal.Destroy()
	}

	pm.cluster.DelMach(pm)
}

func (pm *machine) ConsoleOutput() string {
	if pm.console == nil {
		return ""
	}
	output := pm.console.Output()
	// The provisioning OS boots through iPXE and the real OS boots
	// through GRUB.  Try to ignore console logs from provisioning, but
	// it's better to return everything than nothing.
	grub := strings.Index(output, "GNU GRUB")
	if grub == -1 {
		plog.Warningf("Couldn't find GRUB banner in console output of %s", pm.ID())
		return output
	}
	linux := strings.Index(output[grub:], "Linux version")
	if linux == -1 {
		plog.Warningf("Couldn't find Linux banner in console output of %s", pm.ID())
		return output
	}
	return output[grub+linux:]
}

func (pm *machine) JournalOutput() string {
	if pm.journal == nil {
		return ""
	}

	data, err := pm.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for device %v: %v", pm.ID(), err)
	}
	return string(data)
}
