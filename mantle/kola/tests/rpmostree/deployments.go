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
		// this Ignition config lands the EPEL repo + key
		UserDataV3: conf.Ignition(`{
  "ignition": {
    "version": "3.0.0"
  },
  "storage": {
    "files": [
      {
        "group": {
          "name": "root"
        },
        "path": "/etc/yum.repos.d/epel.repo",
        "user": {
          "name": "root"
        },
        "contents": {
          "source": "data:,%5Bepel%5D%0Aname%3DExtra%20Packages%20for%20Enterprise%20Linux%208%20-%20%24basearch%0Ametalink%3Dhttps%3A%2F%2Fmirrors.fedoraproject.org%2Fmetalink%3Frepo%3Depel-8%26arch%3D%24basearch%0Afailovermethod%3Dpriority%0Aenabled%3D1%0Agpgcheck%3D1%0Agpgkey%3Dfile%3A%2F%2F%2Fetc%2Fpki%2Frpm-gpg%2FRPM-GPG-KEY-EPEL-8%0A"
        },
        "mode": 420
      },
      {
        "group": {
          "name": "root"
        },
        "path": "/etc/pki/rpm-gpg/RPM-GPG-KEY-EPEL-8",
        "user": {
          "name": "root"
        },
        "contents": {
          "source": "data:text/plain;charset=utf-8,-----BEGIN%20PGP%20PUBLIC%20KEY%20BLOCK-----%0D%0A%0D%0AmQINBFz3zvsBEADJOIIWllGudxnpvJnkxQz2CtoWI7godVnoclrdl83kVjqSQp%2B2%0D%0AdgxuG5mUiADUfYHaRQzxKw8efuQnwxzU9kZ70ngCxtmbQWGmUmfSThiapOz00018%0D%0A%2Beo5MFabd2vdiGo1y%2B51m2sRDpN8qdCaqXko65cyMuLXrojJHIuvRA%2Fx7iqOrRfy%0D%0Aa8x3OxC4PEgl5pgDnP8pVK0lLYncDEQCN76D9ubhZQWhISF%2FzJI%2Be806V71hzfyL%0D%0A%2FMt3mQm%2Fli%2BlRKU25Usk9dWaf4NH%2FwZHMIPAkVJ4uD4H%2FuS49wqWnyiTYGT7hUbi%0D%0AecF7crhLCmlRzvJR8mkRP6%2F4T%2FF3tNDPWZeDNEDVFUkTFHNU6%2Fh2%2BO398MNY%2FfOh%0D%0AyKaNK3nnE0g6QJ1dOH31lXHARlpFOtWt3VmZU0JnWLeYdvap4Eff9qTWZJhI7Cq0%0D%0AWm8DgLUpXgNlkmquvE7P2W5EAr2E5AqKQoDbfw%2FGiWdRvHWKeNGMRLnGI3QuoX3U%0D%0ApAlXD7v13VdZxNydvpeypbf%2FAfRyrHRKhkUj3cU1pYkM3DNZE77C5JUe6%2F0nxbt4%0D%0AETUZBTgLgYJGP8c7PbkVnO6I%2FKgL1jw%2B7MW6Az8Ox%2BRXZLyGMVmbW%2FTMc8haJfKL%0D%0AMoUo3TVk8nPiUhoOC0%2FkI7j9ilFrBxBU5dUtF4ITAWc8xnG6jJs%2FIsvRpQARAQAB%0D%0AtChGZWRvcmEgRVBFTCAoOCkgPGVwZWxAZmVkb3JhcHJvamVjdC5vcmc%2BiQI4BBMB%0D%0AAgAiBQJc9877AhsPBgsJCAcDAgYVCAIJCgsEFgIDAQIeAQIXgAAKCRAh6kWrL4bW%0D%0AoWagD%2F4xnLWws34GByVDQkjprk0fX7Iyhpm%2FU7BsIHKspHLL%2BY46vAAGY%2F9vMvdE%0D%0A0fcr9Ek2Zp7zE1RWmSCzzzUgTG6BFoTG1H4Fho%2F7Z8BXK%2FjybowXSZfqXnTOfhSF%0D%0AalwDdwlSJvfYNV9MbyvbxN8qZRU1z7PEWZrIzFDDToFRk0R71zHpnPTNIJ5%2FYXTw%0D%0ANqU9OxII8hMQj4ufF11040AJQZ7br3rzerlyBOB%2BJd1zSPVrAPpeMyJppWFHSDAI%0D%0AWK6x%2Bam13VIInXtqB%2FCz4GBHLFK5d2%2FIYspVw47Solj8jiFEtnAq6%2B1Aq5WH3iB4%0D%0AbE2e6z00DSF93frwOyWN7WmPIoc2QsNRJhgfJC%2BisGQAwwq8xAbHEBeuyMG8GZjz%0D%0Axohg0H4bOSEujVLTjH1xbAG4DnhWO%2F1VXLX%2BLXELycO8ZQTcjj%2F4AQKuo4wvMPrv%0D%0A9A169oETG%2BVwQlNd74VBPGCvhnzwGXNbTK%2FKH1%2BWRH0YSb%2B41flB3NKhMSU6dGI0%0D%0ASGtIxDSHhVVNmx2%2F6XiT9U%2FznrZsG5Kw8nIbbFz%2B9MGUUWgJMsd1Zl9R8gz7V9fp%0D%0An7L7y5LhJ8HOCMsY%2FZ7%2F7HUs%2Bt%2FA1MI4g7Q5g5UuSZdgi0zxukiWuCkLeAiAP4y7%0D%0AzKK4OjJ644NDcWCHa36znwVmkz3ixL8Q0auR15Oqq2BjR%2Ffyog%3D%3D%0D%0A%3D84m8%0D%0A-----END%20PGP%20PUBLIC%20KEY%20BLOCK-----%0A"
        },
        "mode": 420
      }
    ]
  }
}`),
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
		c.MustSSH(m, createBranch)

		// make a commit to the new branch
		createCommit := "sudo ostree commit -b " + newBranch + " --tree ref=" + originalCsum + " --add-metadata-string version=" + newVersion
		newCommit := c.MustSSH(m, createCommit)

		// use "rpm-ostree rebase" to get to the "new" commit
		c.MustSSH(m, "sudo rpm-ostree rebase :"+newBranch)

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
			c.Fatalf("Failed to reboot machine: %v", err)
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
		c.MustSSH(m, "sudo rpm-ostree rollback")

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
		// the two rpmOstreeDeployment structs
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
// 'bird' is available in EPEL(8) and installs on Fedora 29 and RHEL 8
// on all arches
//
// NOTE: we could be churning on the package choice going forward as
// we need something that is a) small, b) has no dependencies, and c)
// can be installed on Fedora + RHEL + all arches from the EPEL repo that we are
// currently using.  We've already had to swap from `fpaste`-`bcrypt`-`bird`
func rpmOstreeInstallUninstall(c cluster.TestCluster) {
	var installPkgName = "bird"
	var installPkgBin string

	if c.Distribution() == "fcos" {
		installPkgBin = "/usr/sbin/bird"
	} else {
		installPkgBin = "/sbin/bird"
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
		c.MustSSH(m, "sudo rpm-ostree install "+installPkgName)

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
		cmdOut := c.MustSSH(m, "command -v "+installPkgName)
		if string(cmdOut) != installPkgBin {
			c.Fatalf(`%q binary in unexpected location. expectd %q, got %q`, installPkgName, installPkgBin, string(cmdOut))
		}

		rpmOut := c.MustSSH(m, "rpm -q "+installPkgName)
		rpmRegex := "^" + installPkgName
		rpmMatch := regexp.MustCompile(rpmRegex).MatchString(string(rpmOut))
		if !rpmMatch {
			c.Fatalf(`Output from "rpm -q" was unexpected: %q`, string(rpmOut))
		}

		// package should be in the metadata
		var reqPkg bool = false
		for _, pkg := range postInstallStatus.Deployments[0].RequestedPackages {
			if pkg == installPkgName {
				reqPkg = true
				break
			}
		}
		if !reqPkg {
			c.Fatalf(`Unable to find "%q" in requested-packages: %v`, installPkgName, postInstallStatus.Deployments[0].RequestedPackages)
		}

		var installPkg bool = false
		for _, pkg := range postInstallStatus.Deployments[0].Packages {
			if pkg == installPkgName {
				installPkg = true
				break
			}
		}
		if !installPkg {
			c.Fatalf(`Unable to find "%q" in packages: %v`, installPkgName, postInstallStatus.Deployments[0].Packages)
		}

		// checksum should be different
		if postInstallStatus.Deployments[0].Checksum == originalCsum {
			c.Fatalf(`Commit IDs incorrectly matched after package install`)
		}
	})

	// uninstall the package
	c.Run("uninstall", func(c cluster.TestCluster) {
		c.MustSSH(m, "sudo rpm-ostree uninstall "+installPkgName)

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
			c.Fatal("Expected %d deployments, got %d", 2, len(postUninstallStatus.Deployments))
		}

		if postUninstallStatus.Deployments[0].Checksum != originalCsum {
			c.Fatalf(`Checksum is incorrect; expected %q, got %q`, originalCsum, postUninstallStatus.Deployments[0].Checksum)
		}

		if len(postUninstallStatus.Deployments[0].RequestedPackages) != 0 {
			c.Fatalf(`Found unexpected requested-packages: %q`, postUninstallStatus.Deployments[0].RequestedPackages)
		}

		if len(postUninstallStatus.Deployments[0].Packages) != 0 {
			c.Fatalf(`Found unexpected packages: %q`, postUninstallStatus.Deployments[0].Packages)
		}

		// cleanup our mess
		cleanupErr := rpmOstreeCleanup(c, m)
		if cleanupErr != nil {
			c.Fatal(cleanupErr)
		}
	})
}
