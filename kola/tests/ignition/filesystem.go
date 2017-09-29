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

package ignition

import (
	"strings"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

const (
	targetUUID   = "9aa5237a-ab6b-458b-a7e8-f25e2baef1a3"
	targetVfatID = "1A37-8FA3"
)

func init() {
	// Reformat the root as btrfs
	btrfsConfigV1 := conf.Ignition(`{
		               "ignitionVersion": 1,
		               "storage": {
		                   "filesystems": [
		                       {
		                           "device": "/dev/disk/by-partlabel/ROOT",
		                           "format": "btrfs",
		                           "create": {
		                               "force": true,
		                               "options": [
		                                   "--label=ROOT",
		                                   "--uuid=` + targetUUID + `"
		                               ]
		                           }
		                       }
		                   ]
		               }
		           }`)
	btrfsConfigV2 := conf.Ignition(`{
		               "ignition": {
		                   "version": "2.0.0"
		               },
		               "storage": {
		                   "filesystems": [
		                       {
		                           "mount": {
		                               "device": "/dev/disk/by-label/ROOT",
		                               "format": "btrfs",
		                               "create": {
		                                   "force": true,
		                                   "options": [
		                                       "--label=ROOT",
		                                       "--uuid=` + targetUUID + `"
		                                   ]
		                               }
		                           }
		                       }
		                   ]
		               }
		           }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.btrfsroot",
		Run:         btrfsRoot,
		ClusterSize: 1,
		UserData:    btrfsConfigV1,
		MinVersion:  semver.Version{Major: 1448},
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.btrfsroot",
		Run:         btrfsRoot,
		ClusterSize: 1,
		UserData:    btrfsConfigV2,
		MinVersion:  semver.Version{Major: 1448},
	})

	// Reformat the root as xfs
	xfsConfigV1 := conf.Ignition(`{
		             "ignitionVersion": 1,
		             "storage": {
		                 "filesystems": [
		                     {
		                         "device": "/dev/disk/by-partlabel/ROOT",
		                         "format": "xfs",
		                         "create": {
		                             "force": true,
		                             "options": [
		                                 "-L", "ROOT",
		                                 "-m", "uuid=` + targetUUID + `"
		                             ]
		                         }
		                     }
		                 ]
		             }
		         }`)
	xfsConfigV2 := conf.Ignition(`{
		             "ignition": {
		                 "version": "2.0.0"
		             },
		             "storage": {
		                 "filesystems": [
		                     {
		                         "mount": {
		                             "device": "/dev/disk/by-partlabel/ROOT",
		                             "format": "xfs",
		                             "create": {
		                                 "force": true,
		                                 "options": [
		                                     "-L", "ROOT",
		                                     "-m", "uuid=` + targetUUID + `"
		                                 ]
		                             }
		                         }
		                     }
		                 ]
		             }
		         }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.xfsroot",
		Run:         xfsRoot,
		ClusterSize: 1,
		UserData:    xfsConfigV1,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.xfsroot",
		Run:         xfsRoot,
		ClusterSize: 1,
		UserData:    xfsConfigV2,
	})

	// Reformat the root as ext4
	ext4ConfigV1 := conf.Ignition(`{
		             "ignitionVersion": 1,
		             "storage": {
		                 "filesystems": [
		                     {
		                         "device": "/dev/disk/by-partlabel/ROOT",
		                         "format": "ext4",
		                         "create": {
		                             "force": true,
		                             "options": [
		                                 "-L", "ROOT",
		                                 "-U", "` + targetUUID + `"
		                             ]
		                         }
		                     }
		                 ]
		             }
		         }`)
	ext4ConfigV2 := conf.Ignition(`{
		             "ignition": {
		                 "version": "2.0.0"
		             },
		             "storage": {
		                 "filesystems": [
		                     {
		                         "mount": {
		                             "device": "/dev/disk/by-partlabel/ROOT",
		                             "format": "ext4",
		                             "create": {
		                                 "force": true,
		                                 "options": [
		                                     "-L", "ROOT",
		                                     "-U", "` + targetUUID + `"
		                                 ]
		                             }
		                         }
		                     }
		                 ]
		             }
		         }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.ext4root",
		Run:         ext4Root,
		ClusterSize: 1,
		UserData:    ext4ConfigV1,
		MinVersion:  semver.Version{Major: 1492},
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4root",
		Run:         ext4Root,
		ClusterSize: 1,
		UserData:    ext4ConfigV2,
		MinVersion:  semver.Version{Major: 1492},
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.ext4checkexisting",
		Run:         ext4CheckExisting,
		ClusterSize: 1,
		UserData:    ext4ConfigV1,
		EndVersion:  semver.Version{Major: 1478},
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4checkexisting",
		Run:         ext4CheckExisting,
		ClusterSize: 1,
		UserData:    ext4ConfigV2,
		EndVersion:  semver.Version{Major: 1478},
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_1.ext4checkexisting",
		Run:         ext4CheckExisting2_1,
		ClusterSize: 1,
		MinVersion:  semver.Version{Major: 1478},
	})

	vfatConfigV2_1 := conf.Ignition(`{
			             "ignition": {
			                 "version": "2.1.0"
			             },
			             "storage": {
			                 "filesystems": [
			                     {
			                         "mount": {
			                             "device": "/dev/disk/by-partlabel/USR-B",
			                             "format": "vfat",
			                             "wipeFilesystem": true,
			                             "label": "USR-B",
			                             "uuid": "` + targetVfatID + `"
			                         }
			                     }
			                 ]
			             }
			         }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_1.vfat",
		Run:         vfatUsrB,
		ClusterSize: 1,
		UserData:    vfatConfigV2_1,
		MinVersion:  semver.Version{Major: 1492},
	})

	swapConfigV2_1 := conf.Ignition(`{
			             "ignition": {
			                 "version": "2.1.0"
			             },
			             "storage": {
			                 "filesystems": [
			                     {
			                         "mount": {
			                             "device": "/dev/disk/by-partlabel/USR-B",
			                             "format": "swap",
			                             "wipeFilesystem": true,
			                             "label": "USR-B",
			                             "uuid": "` + targetUUID + `"
			                         }
			                     }
			                 ]
			             }
			         }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_1.swap",
		Run:         swapUsrB,
		ClusterSize: 1,
		UserData:    swapConfigV2_1,
		MinVersion:  semver.Version{Major: 1492},
	})
}

var ext4NoClobberV2_1 = conf.Ignition(`{
		            "ignition": {
		                "version": "2.1.0"
		            },
		            "storage": {
		                "filesystems": [
		                    {
		                        "mount": {
		                            "device": "/dev/disk/by-partlabel/ROOT",
		                            "format": "ext4",
		                            "label": "ROOT"
		                        }
		                    }
		                ]
		            }
		        }`)

func btrfsRoot(c cluster.TestCluster) {
	testRoot(c, "btrfs")
}

func xfsRoot(c cluster.TestCluster) {
	testRoot(c, "xfs")
}

func ext4Root(c cluster.TestCluster) {
	testRoot(c, "ext4")
}

func vfatUsrB(c cluster.TestCluster) {
	testFormatted(c, "vfat", "USR-B")
}

func swapUsrB(c cluster.TestCluster) {
	testFormatted(c, "swap", "USR-B")
}

func testFormatted(c cluster.TestCluster, fs, label string) {
	m := c.Machines()[0]

	out, err := c.SSH(m, "sudo blkid -s UUID -o value /dev/disk/by-label/"+label)
	if err != nil {
		c.Fatalf("failed to run blkid: %s: %v", out, err)
	}
	target := targetUUID
	if fs == "vfat" {
		target = targetVfatID
	}
	if strings.TrimRight(string(out), "\n") != target {
		c.Fatalf("filesystem wasn't correctly formatted:\n%s", out)
	}

	out, err = c.SSH(m, "sudo blkid -s TYPE -o value /dev/disk/by-label/"+label)
	if err != nil {
		c.Fatalf("failed to run blkid: %s: %v", out, err)
	}
	if strings.TrimRight(string(out), "\n") != fs {
		c.Fatalf("filesystem has incorrect type:\n%s", out)
	}
}

func testRoot(c cluster.TestCluster, fs string) {
	m := c.Machines()[0]

	testFormatted(c, fs, "ROOT")

	out, err := c.SSH(m, "findmnt --noheadings --output FSTYPE --target /")
	if err != nil {
		c.Fatalf("failed to run findmnt: %s: %v", out, err)
	}
	if string(out) != fs {
		c.Fatalf("root wasn't correctly reformatted:\n%s", out)
	}
}

func ext4CheckExisting(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Redirect /dev/null to stdin so isatty(stdin) fails and the -p flag can be
	// checked
	out, err := c.SSH(m, "sudo mkfs.ext4 -p /dev/disk/by-partlabel/ROOT < /dev/null")
	if err == nil {
		c.Fatalf("mkfs.ext4 returned sucessfully when it should have failed")
	}

	if !strings.HasPrefix(string(out), "/dev/disk/by-partlabel/ROOT contains a ext4 file system labelled 'ROOT'") {
		c.Fatalf("mkfs.ext4 did not check for existing filesystems.\nmkfs.ext4: %s", out)
	}
}

func ext4CheckExisting2_1(c cluster.TestCluster) {
	m1 := c.Machines()[0]

	// Get root filesystem UUID
	out, err := c.SSH(m1, "sudo blkid /dev/disk/by-partlabel/ROOT -s UUID -o value")
	if err != nil {
		c.Fatalf("Couldn't get m1 filesystem UUID: %v", err)
	}
	uuid1 := strings.TrimRight(string(out), "\n")

	// Start a machine with config that conditionally recreates the FS
	m2, err := c.NewMachine(ext4NoClobberV2_1)
	if err != nil {
		c.Fatalf("Couldn't start machine: %v", err)
	}

	// Verify UUID hasn't changed
	out, err = c.SSH(m2, "sudo blkid /dev/disk/by-partlabel/ROOT -s UUID -o value")
	if err != nil {
		c.Fatalf("Couldn't get m2 filesystem UUID: %v", err)
	}
	uuid2 := strings.TrimRight(string(out), "\n")
	if uuid1 != uuid2 {
		c.Fatalf("Filesystem was reformatted: %s != %s", uuid1, uuid2)
	}
}
