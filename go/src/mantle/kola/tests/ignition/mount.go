// Copyright 2019 Red Hat
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

package ignition

import (
	"encoding/json"
	"path/filepath"
	"strings"

	ignconverter "github.com/coreos/ign-converter/translate/v30tov22"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
)

var v3IgnitionConfig ignv3types.Config

func init() {
	// mount disks to `/var/log` and `/var/lib/containers`
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.mount.disks",
		Run:         testMountDisks,
		ClusterSize: 0,
		Platforms:   []string{"qemu"},
		Tags:        []string{"ignition"},
	})
	// create new partiitons with disk `vda`
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.mount.partitions",
		Run:         testMountPartitions,
		ClusterSize: 0,
		Platforms:   []string{"qemu"},
		Tags:        []string{"ignition"},
	})
}

// Mount two disks with id `virtio-secondary-disk` and `virtio-tertiary-disk`
// and make sure we can write files to the mount points
func testMountDisks(c cluster.TestCluster) {
	setupIgnitionConfig()

	options := platform.MachineOptions{
		AdditionalDisks: []string{"1024M", "1024M"},
	}

	ignDisks := []ignv3types.Disk{
		{
			Device: "/dev/disk/by-id/virtio-disk1",
			Partitions: []ignv3types.Partition{
				{
					Label:              util.StrToPtr("CONTR"),
					GUID:               util.StrToPtr("63194b49-e4b7-43f9-9a8b-df0fd8279bb7"),
					WipePartitionEntry: util.BoolToPtr(true),
				},
			},
			WipeTable: util.BoolToPtr(true),
		},
		{
			Device: "/dev/disk/by-id/virtio-disk2",
			Partitions: []ignv3types.Partition{
				{
					Label:              util.StrToPtr("LOG"),
					GUID:               util.StrToPtr("6385b84e-2c7b-4488-a870-667c565e01a8"),
					WipePartitionEntry: util.BoolToPtr(true),
				},
			},
			WipeTable: util.BoolToPtr(true),
		},
	}
	createClusterValidate(c, options, ignDisks, 1048576, 512)
}

func testMountPartitions(c cluster.TestCluster) {
	setupIgnitionConfig()

	ignDisks := []ignv3types.Disk{
		{
			Device: "/dev/vda",
			Partitions: []ignv3types.Partition{
				{
					Label:              util.StrToPtr("CONTR"),
					GUID:               util.StrToPtr("63194b49-e4b7-43f9-9a8b-df0fd8279bb7"),
					WipePartitionEntry: util.BoolToPtr(true),
				},
				{
					Label:              util.StrToPtr("LOG"),
					GUID:               util.StrToPtr("6385b84e-2c7b-4488-a870-667c565e01a8"),
					WipePartitionEntry: util.BoolToPtr(true),
				},
			},
			WipeTable: util.BoolToPtr(false),
		},
	}
	createClusterValidate(c, platform.MachineOptions{}, ignDisks, 2097152, 1024)
}

func createClusterValidate(c cluster.TestCluster, options platform.MachineOptions, ignDisks []ignv3types.Disk, v2size int, v3sizeMiB int) {
	var m platform.Machine
	var err error
	v3IgnitionConfig.Storage.Disks = ignDisks

	var serializedConfig []byte
	switch c.IgnitionVersion() {
	case "v2":
		v2ignconfig, err := ignconverter.Translate(v3IgnitionConfig)
		if err != nil {
			break
		}
		for i := 0; i < len(v2ignconfig.Storage.Disks); i++ {
			v2ignconfig.Storage.Disks[i].Partitions[0].Size = v2size
			v2ignconfig.Storage.Disks[i].Partitions[0].Start = 0
		}
		buf, err := json.Marshal(v2ignconfig)
		if err != nil {
			break
		}
		serializedConfig = buf
	case "v3":
		for i := 0; i < len(v3IgnitionConfig.Storage.Disks); i++ {
			v3IgnitionConfig.Storage.Disks[i].Partitions[0].SizeMiB = &v3sizeMiB
			v3IgnitionConfig.Storage.Disks[i].Partitions[0].StartMiB = util.IntToPtr(0)
		}
		buf, err := json.Marshal(v3IgnitionConfig)
		if err != nil {
			break
		}
		serializedConfig = buf
	default:
		c.Fatal("unsupported ignition version")
	}

	if err != nil {
		c.Fatal(err)
	}

	m, err = c.NewMachineWithOptions(conf.Ignition(string(serializedConfig)), options)
	if err != nil {
		c.Fatal(err)
	}

	// make sure partitions are mounted and files are written before rebooting
	mountValidateAll(c, m)

	// reboot it to make sure it comes up again
	err = m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	// make sure partitions are mounted and files are written after rebooting
	mountValidateAll(c, m)
}

func setupIgnitionConfig() {
	containerpartdeviceid := "by-partlabel/CONTR"
	logpartdeviceid := "by-partlabel/LOG"
	if system.RpmArch() == "s390x" {
		containerpartdeviceid = "by-partuuid/63194b49-e4b7-43f9-9a8b-df0fd8279bb7"
		logpartdeviceid = "by-partuuid/6385b84e-2c7b-4488-a870-667c565e01a8"
	}

	v3IgnitionConfig = ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Storage: ignv3types.Storage{
			Filesystems: []ignv3types.Filesystem{
				{
					Device:         "/dev/disk/" + containerpartdeviceid,
					Path:           util.StrToPtr("/var/lib/containers"),
					Label:          util.StrToPtr("CONTR"),
					Format:         util.StrToPtr("xfs"),
					WipeFilesystem: util.BoolToPtr(true),
				},
				{
					Device:         "/dev/disk/" + logpartdeviceid,
					Path:           util.StrToPtr("/var/log"),
					Label:          util.StrToPtr("LOG"),
					Format:         util.StrToPtr("xfs"),
					WipeFilesystem: util.BoolToPtr(true),
				},
			},
			Files: []ignv3types.File{
				{
					Node: ignv3types.Node{
						Path: "/var/lib/containers/hello.txt",
					},
					FileEmbedded1: ignv3types.FileEmbedded1{
						Contents: ignv3types.FileContents{
							Source: util.StrToPtr("data:,hello%20world%0A"),
						},
						Mode: util.IntToPtr(420),
					},
				},
				{
					Node: ignv3types.Node{
						Path: "/var/log/hello.txt",
					},
					FileEmbedded1: ignv3types.FileEmbedded1{
						Contents: ignv3types.FileContents{
							Source: util.StrToPtr("data:,hello%20world%0A"),
						},
						Mode: util.IntToPtr(420),
					},
				},
			},
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "var-lib-containers.mount",
					Contents: util.StrToPtr("[Mount]\nWhat=/dev/disk/" + containerpartdeviceid + "\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"),
					Enabled:  util.BoolToPtr(true),
				},
				{
					Name:     "var-log.mount",
					Contents: util.StrToPtr("[Mount]\nWhat=/dev/disk/" + logpartdeviceid + "\nWhere=/var/log\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"),
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
}

// Check volume correctly mounted to `/var/lib/containers` and `/var/log`
// and test files are written to the filesystem as expected
func mountValidateAll(c cluster.TestCluster, m platform.Machine) {
	mountContents := c.MustSSH(m, "cat /proc/mounts")
	mountValidate(c, m, string(mountContents), "/var/lib/containers")
	mountValidate(c, m, string(mountContents), "/var/log")
}

// Validate partition is mounted to `path` and `path`/hello.txt is written
func mountValidate(c cluster.TestCluster, m platform.Machine, mountContents, path string) {
	if !strings.Contains(mountContents, path+" ") {
		c.Fatalf("No partition mounted to %s", path)
	}

	fPath := filepath.Join(path, "/hello.txt")
	fileContents := c.MustSSH(m, "cat "+fPath)
	if string(fileContents) != "hello world" {
		c.Fatalf("Failed to write content to %s", fPath)
	}
}
