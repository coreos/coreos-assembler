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
	"github.com/coreos/mantle/platform/machine/unprivqemu"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "multipath",
		Run:         runMultipath,
		ClusterSize: 0,
		Platforms:   []string{"qemu-unpriv"},
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

func runMultipath(c cluster.TestCluster) {
	var m platform.Machine
	var err error

	options := platform.QemuMachineOptions{
		MultiPathDisk: true,
	}

	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *unprivqemu.Cluster:
		m, err = pc.NewMachineWithQemuOptions(nil, options)
	default:
		panic("unreachable")
	}
	if err != nil {
		c.Fatal(err)
	}

	c.MustSSH(m, "sudo rpm-ostree kargs --append rd.multipath=default --append root=/dev/disk/by-label/dm-mpath-root")
	err = m.Reboot()
	if err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}

	verifyMultipathBoot(c, m)
}
