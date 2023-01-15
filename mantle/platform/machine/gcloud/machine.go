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

package gcloud

import (
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/coreos-assembler/mantle/platform"
)

type machine struct {
	gc      *cluster
	name    string
	intIP   string
	extIP   string
	dir     string
	journal *platform.Journal
	console string
}

func (gm *machine) ID() string {
	return gm.name
}

func (gm *machine) IP() string {
	return gm.extIP
}

func (gm *machine) PrivateIP() string {
	return gm.intIP
}

func (gm *machine) RuntimeConf() platform.RuntimeConfig {
	return gm.gc.RuntimeConf()
}

func (gm *machine) SSHClient() (*ssh.Client, error) {
	return gm.gc.SSHClient(gm.IP())
}

func (gm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return gm.gc.PasswordSSHClient(gm.IP(), user, password)
}

func (gm *machine) SSH(cmd string) ([]byte, []byte, error) {
	return gm.gc.SSH(gm, cmd)
}

func (gm *machine) IgnitionError() error {
	return nil
}

func (gm *machine) Start() error {
	return platform.StartMachine(gm, gm.journal)
}

func (gm *machine) Reboot() error {
	return platform.RebootMachine(gm, gm.journal)
}

func (gm *machine) WaitForReboot(timeout time.Duration, oldBootId string) error {
	return platform.WaitForMachineReboot(gm, gm.journal, timeout, oldBootId)
}

func (gm *machine) Destroy() {
	if err := gm.saveConsole(); err != nil {
		plog.Errorf("Error saving console for instance %v: %v", gm.ID(), err)
	}

	if err := gm.gc.flight.api.TerminateInstance(gm.name); err != nil {
		plog.Errorf("Error terminating instance %v: %v", gm.ID(), err)
	}

	if gm.journal != nil {
		gm.journal.Destroy()
	}

	gm.gc.DelMach(gm)
}

func (gm *machine) ConsoleOutput() string {
	return gm.console
}

func (gm *machine) saveConsole() error {
	var err error
	gm.console, err = gm.gc.flight.api.GetConsoleOutput(gm.name)
	if err != nil {
		return err
	}

	path := filepath.Join(gm.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(gm.console); err != nil {
		return err
	}

	return nil
}

func (gm *machine) JournalOutput() string {
	if gm.journal == nil {
		return ""
	}

	data, err := gm.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for instance %v: %v", gm.ID(), err)
	}
	return string(data)
}
