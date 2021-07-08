// Copyright 2021 Red Hat, Inc.
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

package misc

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "fcos.installer.cleanup",
		Run:         runInstallerCleanup,
		ClusterSize: 1,
		Distros:     []string{"fcos"},
	})
}

// Old instances might have a leftover Ignition config in /boot/ignition on
// upgrade.  Manually create one, reboot, and ensure that it's correctly
// cleaned up.
// https://github.com/coreos/fedora-coreos-tracker/issues/889
func runInstallerCleanup(c cluster.TestCluster) {
	m := c.Machines()[0]

	c.MustSSH(m, "sudo mount -o remount,rw /boot && sudo mkdir -p /boot/ignition && sudo touch /boot/ignition/config.ign")
	if err := m.Reboot(); err != nil {
		c.Errorf("couldn't reboot: %w", err)
	}
	c.MustSSH(m, "[ ! -e /boot/ignition ]")
}
