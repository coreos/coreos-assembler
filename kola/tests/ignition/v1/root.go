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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	// Reformat the root as btrfs
	btrfsConfig := `{
		               "ignitionVersion": 1,
		               "storage": {
		                   "filesystems": [
		                       {
		                           "device": "/dev/disk/by-partlabel/ROOT",
		                           "format": "btrfs",
		                           "create": {
		                               "force": true,
		                               "options": [
		                                   "--label=ROOT"
		                               ]
		                           }
		                       }
		                   ]
		               }
		           }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.btrfsroot",
		Run:         btrfsRoot,
		ClusterSize: 1,
		UserData:    btrfsConfig,
	})

	// Reformat the root as xfs
	xfsConfig := `{
		             "ignitionVersion": 1,
		             "storage": {
		                 "filesystems": [
		                     {
		                         "device": "/dev/disk/by-partlabel/ROOT",
		                         "format": "xfs",
		                         "create": {
		                             "force": true,
		                             "options": [
		                                 "-L", "ROOT"
		                             ]
		                         }
		                     }
		                 ]
		             }
		         }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.xfsroot",
		Run:         xfsRoot,
		ClusterSize: 1,
		UserData:    xfsConfig,
	})

	// Reformat the root as ext4
	ext4Config := `{
		             "ignitionVersion": 1,
		             "storage": {
		                 "filesystems": [
		                     {
		                         "device": "/dev/disk/by-partlabel/ROOT",
		                         "format": "ext4",
		                         "create": {
		                             "force": true,
		                             "options": [
		                                 "-L", "ROOT"
		                             ]
		                         }
		                     }
		                 ]
		             }
		         }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.ext4root",
		Run:         ext4Root,
		ClusterSize: 1,
		UserData:    ext4Config,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.ext4checkexisting",
		Run:         ext4CheckExisting,
		ClusterSize: 1,
		UserData:    ext4Config,
	})
}

func btrfsRoot(c cluster.TestCluster) {
	testRoot(c, "btrfs")
}

func xfsRoot(c cluster.TestCluster) {
	testRoot(c, "xfs")
}

func ext4Root(c cluster.TestCluster) {
	// Since the image's root partition is formatted to ext4 by default,
	// this test wont be able to differentiate between the original filesystem
	// and a newly created one. If mkfs.ext4 never ran, it would still pass.
	// It will ensure that if mkfs.ext4 ran, it ran successfully.
	testRoot(c, "ext4")
}

func testRoot(c cluster.TestCluster, fs string) {
	m := c.Machines()[0]

	out, err := m.SSH("findmnt --noheadings --output FSTYPE --target /")
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
	out, err := m.SSH("sudo mkfs.ext4 -p /dev/disk/by-partlabel/ROOT < /dev/null")
	if err == nil {
		c.Fatalf("mkfs.ext4 returned sucessfully when it should have failed")
	}

	if !strings.HasPrefix(string(out), "/dev/disk/by-partlabel/ROOT contains a ext4 file system labelled 'ROOT'") {
		c.Fatalf("mkfs.ext4 did not check for existing filesystems.\nmkfs.ext4: %s", out)
	}
}
