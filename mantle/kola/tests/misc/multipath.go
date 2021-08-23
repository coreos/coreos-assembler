// Copyright 2020 Red Hat, Inc.
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
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

var (
	mpath_on_boot_day1 = conf.Butane(`
variant: fcos
version: 1.4.0
kernel_arguments:
  should_exist:
    - rd.multipath=default
    - root=/dev/disk/by-label/dm-mpath-root
    - rw`)
)

func init() {
	register.RegisterTest(&register.Test{
		Name:          "multipath.day1",
		Run:           runMultipathDay1,
		ClusterSize:   1,
		Platforms:     []string{"qemu-unpriv"},
		UserData:      mpath_on_boot_day1,
		MultiPathDisk: true,
	})
	register.RegisterTest(&register.Test{
		Name:          "multipath.day2",
		Run:           runMultipathDay2,
		ClusterSize:   1,
		Platforms:     []string{"qemu-unpriv"},
		MultiPathDisk: true,
	})
}

func verifyMultipathBoot(c cluster.TestCluster, m platform.Machine) {
	for _, mnt := range []string{"/sysroot", "/boot"} {
		verifyMultipath(c, m, mnt)
	}
}

func verifyMultipath(c cluster.TestCluster, m platform.Machine, path string) {
	srcdev := string(c.MustSSHf(m, "findmnt -nvr %s -o SOURCE", path))
	if !strings.HasPrefix(srcdev, "/dev/mapper/mpath") {
		c.Fatalf("mount %s has non-multipath source %s", path, srcdev)
	}
}

func runMultipathDay1(c cluster.TestCluster) {
	m := c.Machines()[0]
	verifyMultipathBoot(c, m)
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	verifyMultipathBoot(c, m)
}

func runMultipathDay2(c cluster.TestCluster) {
	m := c.Machines()[0]
	c.MustSSH(m, "sudo rpm-ostree kargs --append rd.multipath=default --append root=/dev/disk/by-label/dm-mpath-root")
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	verifyMultipathBoot(c, m)
}
