// Copyright 2018 Red Hat, Inc.
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

package rpmostree

import (
	"fmt"
	"reflect"
	"regexp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         rpmOstreeUpgradeRollback,
		ClusterSize: 1,
		Name:        "rpmostree.upgrade-rollback",
		FailFast:    true,
		Tags:        []string{"rpm-ostree", "upgrade"},
	})
	register.RegisterTest(&register.Test{
		Run:         rpmOstreeInstallUninstall,
		ClusterSize: 1,
		Name:        "rpmostree.install-uninstall",
		Tags:        []string{"rpm-ostree"},
		// this Ignition config lands the dummy RPM
		UserData: conf.Ignition(`{
			"ignition": {
			  "version": "3.1.0"
			},
			"storage": {
			  "files": [
				{
				  "path": "/var/home/core/aht-dummy.rpm",
				  "user": {
					"name": "core"
				  },
				  "contents": {
					"source": "https://github.com/projectatomic/atomic-host-tests/raw/master/rpm/aht-dummy-1.0-1.noarch.rpm",
					"verification": {
					  "hash": "sha512-da29ae637b30647cab2386a2ce6b4223c3ad7120ae8dd32d9ce275f26a11946400bba0b86f6feabb9fb83622856ef39f8cecf14b4975638c4d8c0cf33b0f7b26"
					}
				  },
				  "mode": 420
				}
			  ]
			}
		  }
		  `),
		Flags: []register.Flag{register.RequiresInternetAccess}, // these need network to retrieve bits
	})
}

// rpmOstreeUpgradeRollback simulates an upgrade by creating a local branch, making
// a commit to the branch, and rebases the host to said commit.  After a successful
// "upgrade", the host is rolled back to the original deployment.
func rpmOstreeUpgradeRollback(c cluster.TestCluster) {
	var newBranch string = "local-branch"
	var newVersion string = "kola-test-1.0"

	m := c.Machines()[0]

	originalStatus, err := util.GetRpmOstreeStatusJSON(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if len(originalStatus.Deployments) < 1 {
		c.Fatalf(`Unexpected results from "rpm-ostree status"; received: %v`, originalStatus)
	}

	c.Run("upgrade", func(c cluster.TestCluster) {
		// create a local branch to act as our upgrade target
		originalCsum := originalStatus.Deployments[0].Checksum
		createBranch := "sudo ostree refs --create " + newBranch + " " + originalCsum
		c.RunCmdSync(m, createBranch)

		// make a commit to the new branch
		createCommit := "sudo ostree commit -b " + newBranch + " --tree ref=" + originalCsum + " --add-metadata-string version=" + newVersion
		newCommit := c.MustSSH(m, createCommit)

		// use "rpm-ostree rebase" to get to the "new" commit
		c.RunCmdSync(m, "sudo rpm-ostree rebase :"+newBranch)

		// get latest rpm-ostree status output to check validity
		postUpgradeStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		// should have an additional deployment
		if len(postUpgradeStatus.Deployments) != len(originalStatus.Deployments)+1 {
			c.Fatalf("Expected %d deployments; found %d deployments", len(originalStatus.Deployments)+1, len(postUpgradeStatus.Deployments))
		}

		// reboot into new deployment
		rebootErr := m.Reboot()
		if rebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", rebootErr)
		}

		// get latest rpm-ostree status output
		postRebootStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		// should have 2 deployments, the previously booted deployment and the test deployment due to rpm-ostree pruning
		if len(postRebootStatus.Deployments) != 2 {
			c.Fatalf("Expected %d deployments; found %d deployment", 2, len(postRebootStatus.Deployments))
		}

		// origin should be new branch
		if postRebootStatus.Deployments[0].Origin != newBranch {
			c.Fatalf(`New deployment origin is incorrect; expected %q, got %q`, newBranch, postRebootStatus.Deployments[0].Origin)
		}

		// new deployment should be booted
		if !postRebootStatus.Deployments[0].Booted {
			c.Fatalf("New deployment is not reporting as booted")
		}

		// checksum should be new commit
		if postRebootStatus.Deployments[0].Checksum != string(newCommit) {
			c.Fatalf(`New deployment checksum is incorrect; expected %q, got %q`, newCommit, postRebootStatus.Deployments[0].Checksum)
		}

		// version should be new version string
		if postRebootStatus.Deployments[0].Version != newVersion {
			c.Fatalf(`New deployment version is incorrect; expected %q, got %q`, newVersion, postRebootStatus.Deployments[0].Checksum)
		}
	})

	c.Run("rollback", func(c cluster.TestCluster) {
		// rollback to original deployment
		c.RunCmdSync(m, "sudo rpm-ostree rollback")

		newRebootErr := m.Reboot()
		if newRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", err)
		}

		rollbackStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		// still 2 deployments...
		if len(rollbackStatus.Deployments) != 2 {
			c.Fatalf("Expected %d deployments; found %d deployments", 2, len(rollbackStatus.Deployments))
		}

		// validate we are back to the original deployment by comparing the
		// the two RpmOstreeDeployment structs
		if !reflect.DeepEqual(originalStatus.Deployments[0], rollbackStatus.Deployments[0]) {
			c.Fatalf(`Differences found in "rpm-ostree status"; original %v, current: %v`, originalStatus.Deployments[0], rollbackStatus.Deployments[0])
		}

		// cleanup our mess
		cleanupErr := rpmOstreeCleanup(c, m)
		if cleanupErr != nil {
			c.Fatal(cleanupErr)
		}
	})
}

// rpmOstreeInstallUninstall verifies that we can install a package
// and then uninstall it
//
// This uses a dummy RPM that was originally created for the atomic-host-tests;
// see: https://github.com/projectatomic/atomic-host-tests
func rpmOstreeInstallUninstall(c cluster.TestCluster) {
	var ahtRpmPath = "/var/home/core/aht-dummy.rpm"
	var installPkgName = "aht-dummy-1.0-1.noarch"
	var installBinName = "aht-dummy"
	var installBinPath string

	if c.Distribution() == "fcos" {
		installBinPath = fmt.Sprintf("/usr/bin/%v", installBinName)
	} else {
		installBinPath = fmt.Sprintf("/bin/%v", installBinName)
	}

	m := c.Machines()[0]

	originalStatus, err := util.GetRpmOstreeStatusJSON(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if len(originalStatus.Deployments) < 1 {
		c.Fatal(`Unexpected results from "rpm-ostree status"; no deployments?`)
	}

	originalCsum := originalStatus.Deployments[0].Checksum

	c.Run("install", func(c cluster.TestCluster) {
		// install package and reboot
		c.RunCmdSync(m, "sudo rpm-ostree install "+ahtRpmPath)

		installRebootErr := m.Reboot()
		if installRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", installRebootErr)
		}

		postInstallStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		if len(postInstallStatus.Deployments) != 2 {
			c.Fatalf(`Expected two deployments, found %d deployments`, len(postInstallStatus.Deployments))
		}

		// check the command is present, in the rpmdb, and usable
		c.AssertCmdOutputContains(m, "command -v "+installBinName, installBinPath)
		c.AssertCmdOutputMatches(m, "rpm -q "+installPkgName, regexp.MustCompile("^"+installPkgName))

		// package should be in the metadata
		var reqPkg bool = false
		for _, pkg := range postInstallStatus.Deployments[0].RequestedLocalPackages {
			if pkg == installPkgName {
				reqPkg = true
				break
			}
		}
		if !reqPkg {
			c.Fatalf(`Unable to find "%q" in requested-local-packages: %v`, installPkgName, postInstallStatus.Deployments[0].RequestedLocalPackages)
		}

		// checksum should be different
		if postInstallStatus.Deployments[0].Checksum == originalCsum {
			c.Fatalf(`Commit IDs incorrectly matched after package install`)
		}
	})

	// uninstall the package
	c.Run("uninstall", func(c cluster.TestCluster) {
		c.RunCmdSync(m, "sudo rpm-ostree uninstall "+installPkgName)

		uninstallRebootErr := m.Reboot()
		if uninstallRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", uninstallRebootErr)
		}

		postUninstallStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		// check the metadata to make sure everything went well
		if len(postUninstallStatus.Deployments) != 2 {
			c.Fatalf("Expected %d deployments, got %d", 2, len(postUninstallStatus.Deployments))
		}

		if postUninstallStatus.Deployments[0].Checksum != originalCsum {
			c.Fatalf(`Checksum is incorrect; expected %q, got %q`, originalCsum, postUninstallStatus.Deployments[0].Checksum)
		}

		if len(postUninstallStatus.Deployments[0].RequestedLocalPackages) != 0 {
			c.Fatalf(`Found unexpected requested-local-packages: %q`, postUninstallStatus.Deployments[0].RequestedLocalPackages)
		}

		// cleanup our mess
		cleanupErr := rpmOstreeCleanup(c, m)
		if cleanupErr != nil {
			c.Fatal(cleanupErr)
		}
	})
}
