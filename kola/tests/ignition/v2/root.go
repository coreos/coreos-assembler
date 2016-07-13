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
	"fmt"
	"strings"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	// Reformat the root as btrfs
	btrfsConfig := `{
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
		                                       "--label=ROOT"
		                                   ]
		                               }
		                           }
		                       }
		                   ]
		               }
		           }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.btrfsroot.aws",
		Run:         btrfsRoot,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		MinVersion:  semver.Version{Major: 1010},
		UserData:    btrfsConfig,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.btrfsroot.gce",
		Run:         btrfsRoot,
		ClusterSize: 1,
		Platforms:   []string{"gce"},
		MinVersion:  semver.Version{Major: 1045},
		UserData:    btrfsConfig,
	})

	// Reformat the root as xfs
	xfsConfig := `{
		             "ignition": {
		                 "version": "2.0.0"
		             },
		             "storage": {
		                 "filesystems": [
		                     {
		                         "mount": {
		                             "device": "/dev/disk/by-label/ROOT",
		                             "format": "xfs",
		                             "create": {
		                                 "force": true,
		                                 "options": [
		                                     "-L", "ROOT"
		                                 ]
		                             }
		                         }
		                     }
		                 ]
		             }
		         }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.xfsroot.aws",
		Run:         xfsRoot,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		MinVersion:  semver.Version{Major: 1010},
		UserData:    xfsConfig,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.xfsroot.gce",
		Run:         xfsRoot,
		ClusterSize: 1,
		Platforms:   []string{"gce"},
		MinVersion:  semver.Version{Major: 1045},
		UserData:    xfsConfig,
	})

	// Reformat the root as ext4
	ext4Config := `{
		             "ignition": {
		                 "version": "2.0.0"
		             },
		             "storage": {
		                 "filesystems": [
		                     {
		                         "mount": {
		                             "device": "/dev/disk/by-label/ROOT",
		                             "format": "ext4",
		                             "create": {
		                                 "force": true,
		                                 "options": [
		                                     "-L", "ROOT"
		                                 ]
		                             }
		                         }
		                     }
		                 ]
		             }
		         }`
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4Root.aws",
		Run:         ext4Root,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		MinVersion:  semver.Version{Major: 1010},
		UserData:    ext4Config,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4Root.gce",
		Run:         ext4Root,
		ClusterSize: 1,
		Platforms:   []string{"gce"},
		MinVersion:  semver.Version{Major: 1045},
		UserData:    ext4Config,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4CheckExisting.aws",
		Run:         ext4CheckExisting,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		MinVersion:  semver.Version{Major: 1010},
		UserData:    ext4Config,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.ext4CheckExisting.gce",
		Run:         ext4CheckExisting,
		ClusterSize: 1,
		Platforms:   []string{"gce"},
		MinVersion:  semver.Version{Major: 1045},
		UserData:    ext4Config,
	})
}

func btrfsRoot(c platform.TestCluster) error {
	return testRoot(c, "btrfs")
}

func xfsRoot(c platform.TestCluster) error {
	return testRoot(c, "xfs")
}

func ext4Root(c platform.TestCluster) error {
	// Since the image's root partition is formatted to ext4 by default,
	// this test wont be able to differentiate between the original filesystem
	// and a newly created one. If mkfs.ext4 never ran, it would still pass.
	// It will ensure that if mkfs.ext4 ran, it ran successfully.
	return testRoot(c, "ext4")
}

func testRoot(c platform.TestCluster, fs string) error {
	m := c.Machines()[0]

	out, err := m.SSH("findmnt --noheadings --output FSTYPE --target /")
	if err != nil {
		return fmt.Errorf("failed to run findmnt: %s: %v", out, err)
	}

	if string(out) != fs {
		return fmt.Errorf("root wasn't correctly reformatted:\n%s", out)
	}

	return nil
}

func ext4CheckExisting(c platform.TestCluster) error {
	m := c.Machines()[0]

	// Redirect /dev/null to stdin so isatty(stdin) fails and the -p flag can be
	// checked
	out, err := m.SSH("sudo mkfs.ext4 -p /dev/disk/by-label/ROOT < /dev/null")
	if err == nil {
		return fmt.Errorf("mkfs.ext4 returned sucessfully when it should have failed")
	}

	if !strings.HasPrefix(string(out), "/dev/disk/by-label/ROOT contains a ext4 file system labelled 'ROOT'") {
		return fmt.Errorf("mkfs.ext4 did not check for existing filesystems.\nmkfs.ext4: %s", out)
	}

	return nil
}
