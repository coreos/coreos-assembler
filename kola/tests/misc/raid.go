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

package misc

import (
	"encoding/json"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Run:         RootOnRaid,
		ClusterSize: 1,
		Name:        "coreos.disk.raid.root",
		// FIXME: This can only work on qemu, since it's overwriting
		// /usr/share/oem/grub.cfg. The setting being appended (set
		// linux_append="rd.auto") should probably be the default, and this
		// should be removed after the OS level fix is made.
		// https://github.com/coreos/bugs/issues/2099
		Platforms: []string{"qemu"},
		UserData: conf.ContainerLinuxConfig(`storage:
  raid:
    - name: "ROOT"
      level: "raid1"
      devices:
        - "/dev/disk/by-partlabel/ROOT"
        - "/dev/disk/by-partlabel/USR-B"
  filesystems:
    - name: "ROOT"
      mount:
        device: "/dev/md/ROOT"
        format: "ext4"
        create:
          options:
            - "-L"
            - "ROOT"
    - name: "OEM"
      mount:
        device: "/dev/disk/by-label/OEM"
        format: "ext4"
  files:
    - filesystem: "OEM"
      path: "/grub.cfg"
      contents:
        inline: |
            set linux_append="rd.auto"`),
	})
	register.Register(&register.Test{
		Run:         DataOnRaid,
		ClusterSize: 1,
		Name:        "coreos.disk.raid.data",
		UserData: conf.ContainerLinuxConfig(`storage:
  raid:
    - name: "DATA"
      level: "raid1"
      devices:
        - "/dev/disk/by-partlabel/OEM-CONFIG"
        - "/dev/disk/by-partlabel/USR-B"
  filesystems:
    - name: "DATA"
      mount:
        device: "/dev/md/DATA"
        format: "ext4"
        create:
          options:
            - "-L"
            - "DATA"
systemd:
  units:
    - name: "var-lib-data.mount"
      enable: true
      contents: |
          [Mount]
          What=/dev/md/DATA
          Where=/var/lib/data
          Type=ext4
          
          [Install]
          WantedBy=local-fs.target`),
	})
}

func RootOnRaid(c cluster.TestCluster) {
	m := c.Machines()[0]

	checkIfMountpointIsRaid(c, m, "/")

	// reboot it to make sure it comes up again
	err := m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	checkIfMountpointIsRaid(c, m, "/")
}

func DataOnRaid(c cluster.TestCluster) {
	m := c.Machines()[0]

	checkIfMountpointIsRaid(c, m, "/var/lib/data")

	// reboot it to make sure it comes up again
	err := m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	checkIfMountpointIsRaid(c, m, "/var/lib/data")
}

type lsblkOutput struct {
	Blockdevices []blockdevice `json:"blockdevices"`
}

type blockdevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Mountpoint *string       `json:"mountpoint"`
	Children   []blockdevice `json:"children"`
}

// checkIfMountpointIsRaid will check if a given machine has a device of type
// raid1 mounted at the given mountpoint. If it does not, the test is failed.
func checkIfMountpointIsRaid(c cluster.TestCluster, m platform.Machine, mountpoint string) {
	output, err := m.SSH("lsblk --json")
	if err != nil {
		c.Fatalf("couldn't list block devices: %v", err)
	}

	l := lsblkOutput{}
	err = json.Unmarshal(output, &l)
	if err != nil {
		c.Fatalf("couldn't unmarshal lsblk output: %v", err)
	}

	foundRoot := checkIfMountpointIsRaidWalker(c, l.Blockdevices, mountpoint)
	if !foundRoot {
		c.Fatalf("didn't find root mountpoint in lsblk output")
	}
}

// checkIfMountpointIsRaidWalker will iterate over bs and recurse into its
// children, looking for a device mounted at / with type raid1. true is returned
// if such a device is found. The test is failed if a device of a different type
// is found to be mounted at /.
func checkIfMountpointIsRaidWalker(c cluster.TestCluster, bs []blockdevice, mountpoint string) bool {
	for _, b := range bs {
		if b.Mountpoint != nil && *b.Mountpoint == mountpoint {
			if b.Type != "raid1" {
				c.Fatalf("device %q is mounted at %q with type %q (was expecting raid1)", b.Name, mountpoint, b.Type)
			}
			return true
		}
		foundRoot := checkIfMountpointIsRaidWalker(c, b.Children, mountpoint)
		if foundRoot {
			return true
		}
	}
	return false
}
