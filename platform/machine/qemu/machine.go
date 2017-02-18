// Copyright 2016 CoreOS, Inc.
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

package qemu

import (
	"context"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/local"
	"github.com/coreos/mantle/system/exec"
)

type machine struct {
	qc      *Cluster
	id      string
	qemu    exec.Cmd
	netif   *local.Interface
	journal *platform.Journal
}

func (m *machine) ID() string {
	return m.id
}

func (m *machine) IP() string {
	return m.netif.DHCPv4[0].IP.String()
}

func (m *machine) PrivateIP() string {
	return m.netif.DHCPv4[0].IP.String()
}

func (m *machine) SSHClient() (*ssh.Client, error) {
	return m.qc.SSHClient(m.IP())
}

func (m *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return m.qc.PasswordSSHClient(m.IP(), user, password)
}

func (m *machine) SSH(cmd string) ([]byte, error) {
	return m.qc.SSH(m, cmd)
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

func (m *machine) Destroy() error {
	err := m.qemu.Kill()
	if err2 := m.journal.Destroy(); err == nil && err2 != nil {
		err = err2
	}

	m.qc.DelMach(m)

	return err
}
