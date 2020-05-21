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

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         varLibContainers,
		ClusterSize: 0,
		Name:        `coreos.misc.disk.varlibcontainers`,
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
