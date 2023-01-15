// Copyright 2018 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in campliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package azure

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/azure"
)

type machine struct {
	cluster *cluster
	mach    *azure.Machine
	dir     string
	journal *platform.Journal
	console []byte
}

func (am *machine) ID() string {
	return am.mach.ID
}

func (am *machine) IP() string {
	return am.mach.PublicIPAddress
}

func (am *machine) PrivateIP() string {
	return am.mach.PrivateIPAddress
}

func (am *machine) RuntimeConf() platform.RuntimeConfig {
	return am.cluster.RuntimeConf()
}

func (am *machine) ResourceGroup() string {
	return am.cluster.ResourceGroup
}

func (am *machine) InterfaceName() string {
	return am.mach.InterfaceName
}

func (am *machine) PublicIPName() string {
	return am.mach.PublicIPName
}

func (am *machine) SSHClient() (*ssh.Client, error) {
	return am.cluster.SSHClient(am.IP())
}

func (am *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return am.cluster.PasswordSSHClient(am.IP(), user, password)
}

func (am *machine) SSH(cmd string) ([]byte, []byte, error) {
	return am.cluster.SSH(am, cmd)
}

func (am *machine) IgnitionError() error {
	return nil
}

// Re-fetch the Public & Private IP address for the event that it's changed during the reboot
func (am *machine) refetchIPs() error {
	var err error
	am.mach.PublicIPAddress, am.mach.PrivateIPAddress, err = am.cluster.flight.api.GetIPAddresses(am.InterfaceName(), am.PublicIPName(), am.ResourceGroup())
	if err != nil {
		return fmt.Errorf("Fetching IP addresses: %v", err)
	}
	return nil
}

func (am *machine) Start() error {
	return platform.StartMachine(am, am.journal)
}

func (am *machine) Reboot() error {
	err := platform.RebootMachine(am, am.journal)
	if err != nil {
		return err
	}
	return am.refetchIPs()
}

func (am *machine) WaitForReboot(timeout time.Duration, oldBootId string) error {
	err := platform.WaitForMachineReboot(am, am.journal, timeout, oldBootId)
	if err != nil {
		return err
	}
	return am.refetchIPs()
}

func (am *machine) Destroy() {
	if err := am.saveConsole(); err != nil {
		// log error, but do not fail to terminate instance
		plog.Warningf("Saving console for instance %v: %v", am.ID(), err)
	}

	if err := am.cluster.flight.api.TerminateInstance(am.ID(), am.ResourceGroup()); err != nil {
		plog.Errorf("terminating instance: %v", err)
	}

	if am.journal != nil {
		am.journal.Destroy()
	}

	am.cluster.DelMach(am)
}

func (am *machine) ConsoleOutput() string {
	return string(am.console)
}

func (am *machine) saveConsole() error {
	var err error
	am.console, err = am.cluster.flight.api.GetConsoleOutput(am.ID(), am.ResourceGroup(), am.cluster.StorageAccount)
	if err != nil {
		return err
	}

	path := filepath.Join(am.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(am.console)
	if err != nil {
		return fmt.Errorf("failed writing console to file: %v", err)
	}

	return nil
}

func (am *machine) JournalOutput() string {
	if am.journal == nil {
		return ""
	}

	data, err := am.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for machine %v: %v", am.ID(), err)
	}
	return string(data)
}
