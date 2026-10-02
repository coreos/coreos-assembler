// Copyright 2026 Red Hat
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

package stackit

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/stackit"
)

type machine struct {
	cluster *cluster
	server  *stackit.Server
	dir     string
	journal *platform.Journal
	console string
}

var _ platform.Machine = (*machine)(nil)

func (sm *machine) ID() string {
	return sm.server.ID
}

func (sm *machine) IP() string {
	return sm.server.PublicIP
}

func (sm *machine) PrivateIP() string {
	return sm.server.PrivateIP
}

func (sm *machine) RuntimeConf() platform.RuntimeConfig {
	return sm.cluster.RuntimeConf()
}

func (sm *machine) SSHClient() (*ssh.Client, error) {
	return sm.cluster.SSHClient(sm.IP())
}

func (sm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return sm.cluster.PasswordSSHClient(sm.IP(), user, password)
}

func (sm *machine) SSH(cmd string) ([]byte, []byte, error) {
	return sm.cluster.SSH(sm, cmd)
}

func (sm *machine) IgnitionError() error {
	return nil
}

func (sm *machine) Start() error {
	return platform.StartMachine(sm, sm.journal)
}

func (sm *machine) Reboot() error {
	return platform.RebootMachine(sm, sm.journal)
}

func (sm *machine) WaitForReboot(timeout time.Duration, oldBootID string) error {
	return platform.WaitForMachineReboot(sm, sm.journal, timeout, oldBootID)
}

func (sm *machine) WaitForSoftReboot(timeout time.Duration, oldSoftRebootsCount string) error {
	return platform.WaitForMachineSoftReboot(sm, sm.journal, timeout, oldSoftRebootsCount)
}

func (sm *machine) Destroy() {
	if err := sm.saveConsole(); err != nil {
		plog.Errorf("Error saving console for instance %v: %v", sm.ID(), err)
	}

	if err := sm.cluster.flight.api.DeleteServer(sm.server); err != nil {
		plog.Errorf("Error deleting server %v: %v", sm.ID(), err)
	}
	if sm.journal != nil {
		sm.journal.Destroy()
	}
	sm.cluster.DelMach(sm)
}

func (sm *machine) ConsolePath() string {
	return ""
}

func (sm *machine) ConsoleOutput() string {
	return sm.console
}

func (sm *machine) saveConsole() error {
	var err error
	sm.console, err = sm.cluster.flight.api.GetConsoleOutput(sm.ID())
	if err != nil {
		return fmt.Errorf("retrieving console log for %v: %w", sm.ID(), err)
	}
	if sm.dir == "" {
		// Instance setup can fail before the output directory is created.
		return nil
	}
	return os.WriteFile(filepath.Join(sm.dir, "console.txt"), []byte(sm.console), 0644)
}

func (sm *machine) JournalOutput() string {
	if sm.journal == nil {
		return ""
	}
	data, err := sm.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for instance %v: %v", sm.ID(), err)
	}
	return string(data)
}
