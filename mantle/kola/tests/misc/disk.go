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
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
)

const (
	// Extended Boot Loader Partition
	XBootldr = "BC13C2FF-59E6-4262-A352-B275FD6F7172"

	// EFI System Partition (ESP) for UEFI boot
	ESP = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
)

var RootPartition = map[string]string{
	// Root partition for 64-bit x86/AMD64
	"amd64": "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709",
	// Root partition for 64-bit ARM/AArch64 architecture
	"arm64": "B921B045-1DF0-41C3-AF44-4C6F280D3FAE",
	// Root partition for 64-bit PowerPC Big Endian
	"ppc64": "912ADE1D-A839-4913-8964-A10EEE08FBD2",
	// Root partition for 64-bit PowerPC Little Endian
	"ppc64le": "C31C45E6-3F39-412E-80FB-4809C4980599",
	// Root partition for s390x architecture
	"s390x": "5EEAD9A9-FE09-4A1E-A1D7-520D00531306",
	// Root partition for 64-bit RISC-V
	"riscv64": "72EC70A6-CF74-40E6-BD49-4BDA08E8F224",
}[runtime.GOARCH]

func init() {
	register.RegisterTest(&register.Test{
		Run:         varLibContainers,
		ClusterSize: 0,
		Name:        `coreos.misc.disk.varlibcontainers`,
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos", "fcos"},
		Platforms:   []string{"qemu"},
	})

	register.RegisterTest(&register.Test{
		Run:         dpsUuidTest,
		ClusterSize: 0,
		Name:        "coreos.misc.disk.dpsUuid",
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos", "fcos"},
		Platforms:   []string{"qemu"},
	})
}

func varLibContainers(c cluster.TestCluster) {
	var m platform.Machine
	var err error
	disk := "disk1"
	varMountName := "var-lib-containers.mount"
	mkfsServiceName := fmt.Sprintf("systemd-mkfs@dev-disk-by-id-virtio-%s.service", disk)

	options := platform.MachineOptions{
		AdditionalDisks: []string{"1G"},
	}

	mkfsUnit := fmt.Sprintf(`[Unit]
	Description=Make File System on /dev/disk/by-id/%[1]s
	DefaultDependencies=no
	BindsTo=dev-disk-by\x2did-virtio\x2d%[1]s.device
	After=dev-disk-by\x2did-virtio\x2d%[1]s.device var.mount
	Before=systemd-fsck@dev-disk-by\x2did-virtio\x2d%[1]s.service
	Before=shutdown.target

	[Service]
	Type=oneshot
	RemainAfterExit=yes
	ExecStart=/bin/bash -c "/bin/rm -rf /var/lib/containers/*"
	ExecStart=mkfs.xfs /dev/disk/by-id/virtio-%[1]s
	TimeoutSec=0

	[Install]
	WantedBy=var-lib-containers.mount`, disk)

	varMountUnit := fmt.Sprintf(`[Unit]
	Description=Mount %[2]s disk to /var/lib/containers
	Before=local-fs.target
	Requires=%[1]s
	After=%[1]s

	[Mount]
	What=/dev/disk/by-id/virtio-%[2]s
	Where=/var/lib/containers
	Type=xfs
	Options=defaults,prjquota

	[Install]
	WantedBy=local-fs.target`, mkfsServiceName, disk)

	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Storage: ignv3types.Storage{
			Disks: []ignv3types.Disk{
				{
					Device:    "/dev/disk/by-id/virtio-disk1",
					WipeTable: util.BoolToPtr(true),
				},
			},
			Files: []ignv3types.File{
				{
					Node: ignv3types.Node{
						Path: "/var/home/core/Dockerfile",
					},
					FileEmbedded1: ignv3types.FileEmbedded1{
						Contents: ignv3types.FileContents{
							Source: util.StrToPtr("data:,FROM%20scratch%0ACOPY%20os-release%20%2F"),
						},
						Mode: util.IntToPtr(420),
					},
				},
			},
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     mkfsServiceName,
					Contents: &mkfsUnit,
				},
				{
					Name:     varMountName,
					Contents: &varMountUnit,
				},
			},
		},
	}

	serializedConfig, err := json.Marshal(config)
	if err != nil {
		c.Fatalf("Unable to Marshal config")
	}

	m, err = c.NewMachineWithOptions(conf.Ignition(string(serializedConfig)), options)
	if err != nil {
		c.Fatal(err)
	}

	// Verify /var/lib/containers isn't mounted then enable the units to move
	// it to the secondary disk
	vlc := "/var/lib/containers"
	if mount, err := c.SSH(m, fmt.Sprintf("findmnt %s -n -o SOURCE", vlc)); err == nil {
		c.Fatalf("%s is already mounted on %s", vlc, string(mount))
	}

	// build a small container image to exercise /var/lib/container storage
	c.MustSSH(m, "cp /etc/os-release /var/home/core")
	c.MustSSH(m, "sudo podman build -t test -f Dockerfile .")
	c.MustSSH(m, fmt.Sprintf("sudo systemctl enable %s", mkfsServiceName))
	c.MustSSH(m, fmt.Sprintf("sudo systemctl enable %s", varMountName))
	if err := m.Reboot(); err != nil {
		c.Fatal("Unable to reboot")
	}

	// Verify /var/lib/containers is now mounted on the secondary disk
	secondaryDisk := c.MustSSH(m, fmt.Sprintf("readlink -f /dev/disk/by-id/virtio-%s", disk))
	mount := c.MustSSH(m, fmt.Sprintf("findmnt %s -n -o SOURCE", vlc))
	if string(secondaryDisk) != string(mount) {
		c.Fatalf("%s is not mounted on %s", vlc, secondaryDisk)
	}
	c.MustSSH(m, "sudo podman build -t test -f Dockerfile .")
}

type LSBLK struct {
	Blockdevices []BlockDevice `json:"blockdevices"`
}

type BlockDevice struct {
	Name         string        `json:"name"`
	PartType     *string       `json:"parttype"`
	UUID         *string       `json:"uuid"`
	Mountpoint   *string       `json:"mountpoint"`
	PartTypeName *string       `json:"parttypename"`
	Label        *string       `json:"label"`
	Children     []BlockDevice `json:"children,omitempty"`
}

func dpsFatalFail(c cluster.TestCluster, blockDev *BlockDevice, reqUuid string) {
	c.Fatalf(
		"Partition '%s' does not have DPS UUID. Have: %s, Need: %s",
		blockDev.Name,
		*blockDev.PartType,
		reqUuid,
	)
}

func dpsUuidTest(c cluster.TestCluster) {
	c.Platform()

	m, err := c.NewMachine(nil)
	if err != nil {
		c.Fatal(err)
	}

	// find the partition
	devMajMin := strings.TrimSpace(string(c.MustSSH(m, "findmnt -no MAJ:MIN /sysroot")))

	// find the backing dev
	cmd := fmt.Sprintf("basename $(dirname $(readlink -f /sys/dev/block/%s))", devMajMin)
	device := strings.TrimSpace(string(c.MustSSH(m, cmd)))

	cmd = fmt.Sprintf(
		"lsblk -oname,parttype,uuid,mountpoint,parttypename,label --json /dev/%s",
		device,
	)
	lsblkOut := c.MustSSH(m, cmd)

	lsblk := LSBLK{}

	err = json.Unmarshal(lsblkOut, &lsblk)
	if err != nil {
		c.Fatal(err)
	}

	if len(lsblk.Blockdevices) != 1 {
		c.Fatalf("More than one block device found for device no '%s'", devMajMin)
	}

	// NOTE: Not adding a check for whether we found the XBootldr
	// partition or not as there would be a few cases where it won't be used
	// Ex. In composefs-native UKI case
	var (
		rootFound = false
		espFound  = false
	)

	for _, child := range lsblk.Blockdevices[0].Children {
		if child.Label == nil {
			continue
		}

		if child.PartType == nil {
			c.Fatalf("'%s' has no parttype", child.Name)
		}

		switch *child.Label {
		case "boot":
			if strings.ToUpper(*child.PartType) != XBootldr {
				dpsFatalFail(c, &child, XBootldr)
			}

		case "root":
			rootFound = true
			if strings.ToUpper(*child.PartType) != RootPartition {
				dpsFatalFail(c, &child, RootPartition)
			}

		case "EFI-SYSTEM":
			espFound = true
			if strings.ToUpper(*child.PartType) != ESP {
				dpsFatalFail(c, &child, ESP)
			}
		}
	}

	if !rootFound {
		c.Fatalf("Root partition not found")
	}

	if !espFound {
		c.Fatalf("EFI partition not found")
	}
}
