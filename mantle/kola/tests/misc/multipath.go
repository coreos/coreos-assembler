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
	mpath_on_var_lib_containers = conf.Butane(`
variant: fcos
version: 1.4.0
systemd:
  units:
    - name: mpath-configure.service
      enabled: true
      contents: |
        [Unit]
        Description=Configure Multipath
        ConditionFirstBoot=true
        ConditionPathExists=!/etc/multipath.conf
        Before=multipathd.service
        DefaultDependencies=no

        [Service]
        Type=oneshot
        ExecStart=/usr/sbin/mpathconf --enable

        [Install]
        WantedBy=multi-user.target
    - name: mpath-var-lib-containers.service
      enabled: true
      contents: |
        [Unit]
        Description=Set Up Multipath On /var/lib/containers
        ConditionFirstBoot=true
        Requires=dev-mapper-mpatha.device
        After=dev-mapper-mpatha.device
        Before=kubelet.service
        DefaultDependencies=no

        [Service]
        Type=oneshot
        ExecStart=/usr/sbin/mkfs.xfs -L containers -m reflink=1 /dev/mapper/mpatha

        [Install]
        WantedBy=multi-user.target
    - name: var-lib-containers.mount
      enabled: true
      contents: |
        [Unit]
        Description=Mount /var/lib/containers
        # See https://github.com/coreos/coreos-assembler/pull/2457
        After=ostree-remount.service
        After=mpath-var-lib-containers.service
        Before=kubelet.service

        [Mount]
        What=/dev/disk/by-label/dm-mpath-containers
        Where=/var/lib/containers
        Type=xfs

        [Install]
        WantedBy=multi-user.target`)
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
	register.RegisterTest(&register.Test{
		Name:            "multipath.partition",
		Run:             runMultipathPartition,
		ClusterSize:     1,
		Platforms:       []string{"qemu-unpriv"},
		UserData:        mpath_on_var_lib_containers,
		AdditionalDisks: []string{"1G:mpath"},
	})
}

func verifyMultipathBoot(c cluster.TestCluster, m platform.Machine) {
	for _, mnt := range []string{"/sysroot", "/boot"} {
		verifyMultipath(c, m, mnt)
	}
	c.MustSSH(m, "test -f /etc/multipath.conf")
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
	c.RunCmdSync(m, "sudo rpm-ostree kargs --append rd.multipath=default --append root=/dev/disk/by-label/dm-mpath-root")
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	verifyMultipathBoot(c, m)
}

func runMultipathPartition(c cluster.TestCluster) {
	m := c.Machines()[0]
	verifyMultipath(c, m, "/var/lib/containers")
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	verifyMultipath(c, m, "/var/lib/containers")
}
