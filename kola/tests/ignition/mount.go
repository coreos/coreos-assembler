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
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
)

var (
	v2IgnitionConfig = conf.Ignition(`{
		"ignition": {
			"version": "2.2.0"
		},
		"storage": {
			"disks": [
				{
					"device": "/dev/disk/by-id/virtio-secondary-disk",
					"wipeTable": true,
					"partitions": [
						{
							"label": "CONTR",
							"size": 1048576,
							"start": 0
						}
					]
				},
				{
					"device": "/dev/disk/by-id/virtio-tertiary-disk",
					"wipeTable": true,
					"partitions": [
						{
							"label": "LOG",
							"size": 1048576,
							"start": 0
						}
					]
				}
			],
			"filesystems": [
				{
					"name": "CONTR",
					"mount": {
						"device": "/dev/disk/by-partlabel/CONTR",
						"format": "xfs",
						"wipeFilesystem": true
					}
				},
				{
					"name": "LOG",
					"mount": {
						"device": "/dev/disk/by-partlabel/LOG",
						"format": "xfs",
						"wipeFilesystem": true
					}
				}
			],
			"files": [
				{
					"filesystem": "CONTR",
					"path": "/hello.txt",
					"contents": {
						"source": "data:,hello%20world%0A"
					},
					"mode": 420
				},
				{
					"filesystem": "LOG",
					"path": "/hello.txt",
					"contents": {
						"source": "data:,hello%20world%0A"
					},
					"mode": 420
				}
			]
		},
		"systemd": {
			"units": [
				{
					"name": "var-lib-containers.mount",
					"enabled": true,
					"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/CONTR\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
				},
				{
					"name": "var-log.mount",
					"enabled": true,
					"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/LOG\nWhere=/var/log\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
				}
			]
		}
	}`)
	v3IgnitionConfig = conf.Ignition(`{
		"ignition": {"version": "3.0.0"},
		"storage": {
			"disks": [
				{
					"device": "/dev/disk/by-id/virtio-secondary-disk",
					"wipeTable": true,
					"partitions": [
						{
							"label": "CONTR",
							"sizeMiB": 512,
							"startMiB": 0,
							"wipePartitionEntry": true
						}
					]
				},
				{
					"device": "/dev/disk/by-id/virtio-tertiary-disk",
					"wipeTable": true,
					"partitions": [
						{
							"label": "LOG",
							"sizeMiB": 512,
							"startMiB": 0,
							"wipePartitionEntry": true
						}
					]
				}
			],
			"filesystems": [
				{
					"path": "/var/lib/containers",
					"device": "/dev/disk/by-partlabel/CONTR",
					"format": "xfs",
					"label": "CONTR",
					"wipeFilesystem": true
				},
				{
					"path": "/var/log",
					"device": "/dev/disk/by-partlabel/LOG",
					"format": "xfs",
					"label": "LOG",
					"wipeFilesystem": true
				}
			],
			"files": [
				{
					"path": "/var/lib/containers/hello.txt",
					"contents": {
						"source": "data:,hello%20world%0A"
					}
				},
				{
					"path": "/var/log/hello.txt",
					"contents": {
						"source": "data:,hello%20world%0A"
					}
				}
			]
		},
		"systemd": {
			"units": [
				{
					"name": "var-lib-containers.mount",
					"enabled": true,
					"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/CONTR\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
				},
				{
					"name": "var-log.mount",
					"enabled": true,
					"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/LOG\nWhere=/var/log\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
				}]
		}
	}`)
)

func init() {
	// mount disks to `/var/log` and `/var/lib/containers`
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.mount.disks",
		Run:         testMountDisks,
		ClusterSize: 0,
		Platforms:   []string{"qemu"},
	})
	// create new partiitons with disk `vda`
	register.RegisterTest(&register.Test{
		Name: "coreos.ignition.mount.partitions",
		Run:  testMountPartitions,
		UserData: conf.Ignition(`{
			"ignition": {
				"version": "2.2.0"
			},
			"storage": {
				"disks": [
					{
						"device": "/dev/vda",
						"wipeTable": false,
						"partitions": [
							{
								"label": "CONTR",
								"size": 2097152,
								"start": 0
							},
							{
								"label": "LOG",
								"size": 2097152,
								"start": 0
							}
						]
					}
				],
				"filesystems": [
					{
						"name": "CONTR",
						"mount": {
							"device": "/dev/disk/by-partlabel/CONTR",
							"format": "xfs",
							"wipeFilesystem": true
						}
					},
					{
						"name": "LOG",
						"mount": {
							"device": "/dev/disk/by-partlabel/LOG",
							"format": "xfs",
							"wipeFilesystem": true
						}
					}
				],
				"files": [
					{
						"filesystem": "CONTR",
						"path": "/hello.txt",
						"contents": {
							"source": "data:,hello%20world%0A"
						},
						"mode": 420
					},
					{
						"filesystem": "LOG",
						"path": "/hello.txt",
						"contents": {
							"source": "data:,hello%20world%0A"
						},
						"mode": 420
					}
				]
			},
			"systemd": {
				"units": [
					{
						"name": "var-lib-containers.mount",
						"enabled": true,
						"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/CONTR\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
					},
					{
						"name": "var-log.mount",
						"enabled": true,
						"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/LOG\nWhere=/var/log\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
					}
				]
			}
		}`),
		UserDataV3: conf.Ignition(`{
			"ignition": {"version": "3.0.0"},
			"storage": {
				"disks": [
					{
						"device": "/dev/vda",
						"wipeTable": false,
						"partitions": [
						{
							"label": "CONTR",
							"sizeMiB": 1024,
							"startMiB": 0,
							"wipePartitionEntry": true
						},
						{
							"label": "LOG",
							"sizeMiB": 1024,
							"startMiB": 0,
							"wipePartitionEntry": true
						}
						]
					}
				],
				"filesystems": [
					{
						"path": "/var/lib/containers",
						"device": "/dev/disk/by-partlabel/CONTR",
						"format": "xfs",
						"label": "CONTR",
						"wipeFilesystem": true
					},
					{
						"path": "/var/log",
						"device": "/dev/disk/by-partlabel/LOG",
						"format": "xfs",
						"label": "LOG",
						"wipeFilesystem": true
					}
				],
				"files": [
					{
						"path": "/var/lib/containers/hello.txt",
						"contents": {
							"source": "data:,hello%20world%0A"
						}
					},
					{
						"path": "/var/log/hello.txt",
						"contents": {
							"source": "data:,hello%20world%0A"
						}
					}
				]
			},
			"systemd": {
				"units": [
					{
						"name": "var-lib-containers.mount",
						"enabled": true,
						"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/CONTR\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
					},
					{
						"name": "var-log.mount",
						"enabled": true,
						"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/LOG\nWhere=/var/log\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target"
					}]
			}
		}`),
		ClusterSize: 1,
		Platforms:   []string{"qemu"},
	})
}

// Mount two disks with id `virtio-secondary-disk` and `virtio-tertiary-disk`
// and make sure we can write files to the mount points
func testMountDisks(c cluster.TestCluster) {
	var m platform.Machine
	var err error
	var ignConfig *conf.UserData

	options := platform.MachineOptions{
		AdditionalDisks: []platform.Disk{
			{Size: "1024M", DeviceOpts: []string{"serial=secondary-disk"}},
			{Size: "1024M", DeviceOpts: []string{"serial=tertiary-disk"}},
		},
	}

	// TODO: use translation between Ignition v2 and v3 in the future
	switch c.IgnitionVersion() {
	case "v2":
		ignConfig = v2IgnitionConfig
	case "v3":
		ignConfig = v3IgnitionConfig
	default:
		c.Fatal("unsupported ignition version")
	}

	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithOptions function, but rather whether platform.Cluster does which fails
	case *unprivqemu.Cluster:
		m, err = pc.NewMachineWithOptions(ignConfig, options, true)
	default:
		c.Fatal("unknown cluster type")
	}

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

func testMountPartitions(c cluster.TestCluster) {
	m := c.Machines()[0]

	// make sure partitions are mounted and files are written
	mountValidateAll(c, m)

	// reboot it to make sure it comes up again
	err := m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	// make sure partitions are mounted and files are written
	mountValidateAll(c, m)
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
