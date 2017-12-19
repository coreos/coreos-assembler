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

package oci

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/oci"
)

type machine struct {
	cluster *cluster
	mach    *oci.Machine
	dir     string
	journal *platform.Journal
	console string
}

func (om *machine) ID() string {
	return om.mach.ID
}

func (om *machine) IP() string {
	return om.mach.PublicIPAddress
}

func (om *machine) PrivateIP() string {
	return om.mach.PrivateIPAddress
}

func (om *machine) RuntimeConf() platform.RuntimeConfig {
	return om.cluster.RuntimeConf()
}

func (om *machine) SSHClient() (*ssh.Client, error) {
	return om.cluster.SSHClient(om.IP())
}

func (om *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return om.cluster.PasswordSSHClient(om.IP(), user, password)
}

func (om *machine) SSH(cmd string) ([]byte, []byte, error) {
	return om.cluster.SSH(om, cmd)
}

func (om *machine) Reboot() error {
	return platform.RebootMachine(om, om.journal, om.cluster.RuntimeConf())
}

func (om *machine) Destroy() {
	if err := om.saveConsole(); err != nil {
		plog.Errorf("Error saving console for instance %v: %v", om.ID(), err)
	}

	if err := om.cluster.api.TerminateInstance(om.ID()); err != nil {
		plog.Errorf("Error terminating instance %v: %v", om.ID(), err)
	}

	if om.journal != nil {
		om.journal.Destroy()
	}

	om.cluster.DelMach(om)
}

func (om *machine) ConsoleOutput() string {
	return om.console
}

func (om *machine) saveConsole() error {
	var err error
	om.console, err = om.cluster.api.GetConsoleOutput(om.ID())
	if err != nil {
		return err
	}

	path := filepath.Join(om.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(om.console)
	if err != nil {
		return fmt.Errorf("failed writing console to file: %v", err)
	}

	return nil
}
