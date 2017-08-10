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

package esx

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/esx"
)

type machine struct {
	cluster *cluster
	mach    *esx.ESXMachine
	dir     string
	journal *platform.Journal
	console string
}

func (em *machine) ID() string {
	return em.mach.Name
}

func (em *machine) IP() string {
	return em.mach.IPAddress
}

func (em *machine) PrivateIP() string {
	return em.mach.IPAddress
}

func (em *machine) SSHClient() (*ssh.Client, error) {
	return em.cluster.SSHClient(em.IP())
}

func (em *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return em.cluster.PasswordSSHClient(em.IP(), user, password)
}

func (em *machine) SSH(cmd string) ([]byte, error) {
	return em.cluster.SSH(em, cmd)
}

func (m *machine) Reboot() error {
	return platform.RebootMachine(m, m.journal, m.cluster.RuntimeConf())
}

func (em *machine) Destroy() error {
	if err := em.cluster.api.TerminateDevice(em.ID()); err != nil {
		return err
	}

	if em.journal != nil {
		if err := em.journal.Destroy(); err != nil {
			return err
		}
	}

	if err := em.saveConsole(); err != nil {
		return err
	}

	em.cluster.DelMach(em)

	return nil
}

func (em *machine) ConsoleOutput() string {
	return em.console
}

func (em *machine) saveConsole() error {
	var err error
	em.console, err = em.cluster.api.GetConsoleOutput(em.ID())
	if err != nil {
		return err
	}

	path := filepath.Join(em.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(em.console)
	if err != nil {
		return fmt.Errorf("failed writing console to file: %v", err)
	}

	return nil
}
