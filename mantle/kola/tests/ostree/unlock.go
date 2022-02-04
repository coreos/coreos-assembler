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

package ostree

import (
	"fmt"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         ostreeUnlockTest,
		ClusterSize: 1,
		Name:        "ostree.unlock",
		Flags:       []register.Flag{register.RequiresInternetAccess}, // need network to pull RPM
		FailFast:    true,
		Tags:        []string{"ostree"},
	})
	register.RegisterTest(&register.Test{
		Run:         ostreeHotfixTest,
		ClusterSize: 1,
		Flags:       []register.Flag{register.RequiresInternetAccess}, // need network to pull RPM
		Name:        "ostree.hotfix",
		FailFast:    true,
		Tags:        []string{"ostree"},
		// enable debugging for https://github.com/coreos/fedora-coreos-tracker/issues/942
		// we can drop it once we resolved it
		UserData: conf.Butane(`
variant: fcos
version: 1.4.0
systemd:
  units:
  - name: rpm-ostreed.service
    dropins:
    - name: 10-debug.conf
      contents: |-
        [Service]
        Environment=G_MESSAGES_DEBUG=rpm-ostreed`),
	})

}

var (
	rpmUrl  string = "https://raw.githubusercontent.com/projectatomic/atomic-host-tests/master/rpm/aht-dummy-1.0-1.noarch.rpm"
	rpmName string = "aht-dummy"
)

// ostreeAdminUnlock will unlock the deployment and verify the success of the operation
func ostreeAdminUnlock(c cluster.TestCluster, m platform.Machine, hotfix bool) error {
	var unlockCmd string = "sudo ostree admin unlock"
	if hotfix {
		unlockCmd = "sudo ostree admin unlock --hotfix"
	}

	c.RunCmdSync(m, unlockCmd)

	// just sanity-check that SSH itself still works
	// https://github.com/coreos/fedora-coreos-tracker/issues/942
	c.RunCmdSync(m, "true")

	status, err := util.GetRpmOstreeStatusJSON(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if len(status.Deployments) < 1 {
		c.Fatalf(`Did not find any deployments`)
	}

	// verify that the unlock was successful
	if hotfix && status.Deployments[0].Unlocked != "hotfix" {
		return fmt.Errorf(`Hotfix mode is not reflected in "rpm-ostree status"; got: %q`, status.Deployments[0].Unlocked)
	}

	if !hotfix && status.Deployments[0].Unlocked != "development" {
		return fmt.Errorf(`Unlocked mode is not reflected in "rpm-ostree status"; got: %q`, status.Deployments[0].Unlocked)
	}

	return nil
}

// rpmInstallVerify is a small utility func to handle installing an RPM
// and verifying the install was successful
// NOTE: RPM name and binary name must match
func rpmInstallVerify(c cluster.TestCluster, m platform.Machine, rpmFile string, rpmName string) error {
	_, installErr := c.SSH(m, ("sudo rpm -i " + rpmFile))
	if installErr != nil {
		return fmt.Errorf(`Failed to install RPM: %v`, installErr)
	}

	_, cmdErr := c.SSH(m, ("command -v " + rpmName))
	if cmdErr != nil {
		return fmt.Errorf(`Failed to find binary: %v`, cmdErr)
	}

	_, rpmErr := c.SSH(m, ("rpm -q " + rpmName))
	if rpmErr != nil {
		return fmt.Errorf(`Failed to find RPM in rpmdb: %v`, rpmErr)
	}

	return nil
}

// rpmUninstallVerify is a small utility func to handle uninstalling an RPM
// and verifying the uninstall was successful
// NOTE: RPM name and binary name must match
func rpmUninstallVerify(c cluster.TestCluster, m platform.Machine, rpmName string) error {
	_, uninstallErr := c.SSH(m, ("sudo rpm -e " + rpmName))
	if uninstallErr != nil {
		return fmt.Errorf(`Failed to uninstall RPM: %v`, uninstallErr)
	}

	_, missCmdErr := c.SSH(m, ("command -v " + rpmName))
	if missCmdErr == nil {
		return fmt.Errorf(`Found a binary that should not be there: %v`, missCmdErr)
	}

	_, missRpmErr := c.SSH(m, ("rpm -q " + rpmName))
	if missRpmErr == nil {
		return fmt.Errorf(`RPM incorrectly in rpmdb after RPM uninstall: %v`, missRpmErr)
	}

	return nil
}

// ostreeUnlockTest verifies the simplest use of `ostree admin unlock` by
// trying to install a dummy RPM on the host, rebooting, and ensuring
// the RPM is gone after reboot
func ostreeUnlockTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// unlock the deployment
	c.Run("unlock", func(c cluster.TestCluster) {
		unlockErr := ostreeAdminUnlock(c, m, false)
		if unlockErr != nil {
			c.Fatal(unlockErr)
		}
	})

	// try to install an RPM via HTTP
	c.Run("install", func(c cluster.TestCluster) {
		rpmInstallErr := rpmInstallVerify(c, m, rpmUrl, rpmName)
		if rpmInstallErr != nil {
			c.Fatal(rpmInstallErr)
		}
	})

	// try to uninstall RPM
	c.Run("uninstall", func(c cluster.TestCluster) {
		rpmUninstallErr := rpmUninstallVerify(c, m, rpmName)
		if rpmUninstallErr != nil {
			c.Fatal(rpmUninstallErr)
		}
	})

	// re-install the RPM and verify the unlocked deployment is discarded
	// after reboot
	c.Run("discard", func(c cluster.TestCluster) {
		c.RunCmdSync(m, ("sudo rpm -i " + rpmUrl))

		unlockRebootErr := m.Reboot()
		if unlockRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", unlockRebootErr)
		}

		ros, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		if ros.Deployments[0].Unlocked != "none" {
			c.Fatalf(`Deployment was incorrectly unlocked; got: %q`, ros.Deployments[0].Unlocked)
		}

		_, secCmdErr := c.SSH(m, ("command -v " + rpmName))
		if secCmdErr == nil {
			c.Fatalf(`Binary was incorrectly found after reboot`)
		}
		_, secRpmErr := c.SSH(m, ("rpm -q " + rpmName))
		if secRpmErr == nil {
			c.Fatalf(`RPM incorrectly in rpmdb after reboot`)
		}
	})
}

// ostreeHotfixTest verifies that the deployment can be put into "hotfix"
// mode, where the RPMs installed will persist across reboots.  A rollback will
// return the host to the original state
func ostreeHotfixTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// unlock the deployment into "hotfix" mode
	c.RunLogged("unlock", func(c cluster.TestCluster) {
		unlockErr := ostreeAdminUnlock(c, m, true)
		if unlockErr != nil {
			c.Fatal(unlockErr)
		}
	})

	// try to install an RPM via HTTP
	c.RunLogged("install", func(c cluster.TestCluster) {
		rpmInstallErr := rpmInstallVerify(c, m, rpmUrl, rpmName)
		if rpmInstallErr != nil {
			c.Fatal(rpmInstallErr)
		}
	})

	// uninstall the RPM
	c.RunLogged("uninstall", func(c cluster.TestCluster) {
		rpmUninstallErr := rpmUninstallVerify(c, m, rpmName)
		if rpmUninstallErr != nil {
			c.Fatal(rpmUninstallErr)
		}
	})

	// install the RPM again, reboot, verify it the "hotfix" deployment
	// and RPM have persisted
	c.RunLogged("persist", func(c cluster.TestCluster) {
		c.RunCmdSync(m, ("sudo rpm -i " + rpmUrl))

		unlockRebootErr := m.Reboot()
		if unlockRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", unlockRebootErr)
		}

		ros, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		if ros.Deployments[0].Unlocked != "hotfix" {
			c.Fatalf(`Hotfix mode was not detected; got: %q`, ros.Deployments[0].Unlocked)
		}

		c.RunCmdSync(m, ("command -v " + rpmName))

		c.RunCmdSync(m, ("rpm -q " + rpmName))
	})

	// roll back the deployment and verify the "hotfix" is no longer present
	c.RunLogged("rollback", func(c cluster.TestCluster) {
		c.RunCmdSync(m, "sudo rpm-ostree rollback")

		rollbackRebootErr := m.Reboot()
		if rollbackRebootErr != nil {
			c.Fatalf("Failed to reboot machine: %v", rollbackRebootErr)
		}

		rollbackStatus, err := util.GetRpmOstreeStatusJSON(c, m)
		if err != nil {
			c.Fatal(err)
		}

		if rollbackStatus.Deployments[0].Unlocked != "none" {
			c.Fatalf(`Rollback did not remove hotfix mode; got: %q`, rollbackStatus.Deployments[0].Unlocked)
		}

		_, secCmdErr := c.SSH(m, ("command -v " + rpmName))
		if secCmdErr == nil {
			c.Fatalf(`Binary was incorrectly found after reboot`)
		}
		_, secRpmErr := c.SSH(m, ("rpm -q " + rpmName))
		if secRpmErr == nil {
			c.Fatalf(`RPM incorrectly in rpmdb after reboot`)
		}
	})
}
