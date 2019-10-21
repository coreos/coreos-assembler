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
	register.Register(&register.Test{
		Run:         rpmOstreeUpgradeRollback,
		ClusterSize: 1,
		Name:        "rpmostree.upgrade-rollback",
		Distros:     []string{"fcos", "rhcos"},
		FailFast:    true,
	})
	register.Register(&register.Test{
		Run:         rpmOstreeInstallUninstall,
		ClusterSize: 1,
		Name:        "rpmostree.install-uninstall",
		// this Ignition config lands the EPEL repo + key
		UserData: conf.Ignition(`{
  "ignition": {
    "version": "2.2.0"
  },
  "storage": {
    "files": [
      {
        "filesystem": "root",
        "group": {
          "name": "root"
        },
        "path": "/etc/yum.repos.d/epel.repo",
        "user": {
          "name": "root"
        },
        "contents": {
          "source": "data:,%5Bepel%5D%0Aname%3DExtra%20Packages%20for%20Enterprise%20Linux%207%20-%20%24basearch%0Ametalink%3Dhttps%3A%2F%2Fmirrors.fedoraproject.org%2Fmetalink%3Frepo%3Depel-7%26arch%3D%24basearch%0Afailovermethod%3Dpriority%0Aenabled%3D1%0Agpgcheck%3D1%0Agpgkey%3Dfile%3A%2F%2F%2Fetc%2Fpki%2Frpm-gpg%2FRPM-GPG-KEY-EPEL-7%0A"
        },
        "mode": 420
      },
      {
        "filesystem": "root",
        "group": {
          "name": "root"
        },
        "path": "/etc/pki/rpm-gpg/RPM-GPG-KEY-EPEL-7",
        "user": {
          "name": "root"
        },
        "contents": {
          "source": "data:,-----BEGIN%20PGP%20PUBLIC%20KEY%20BLOCK-----%0AVersion%3A%20GnuPG%20v1.4.11%20(GNU%2FLinux)%0A%0AmQINBFKuaIQBEAC1UphXwMqCAarPUH%2FZsOFslabeTVO2pDk5YnO96f%2BrgZB7xArB%0AOSeQk7B90iqSJ85%2Fc72OAn4OXYvT63gfCeXpJs5M7emXkPsNQWWSju99lW%2BAqSNm%0AjYWhmRlLRGl0OO7gIwj776dIXvcMNFlzSPj00N2xAqjMbjlnV2n2abAE5gq6VpqP%0AvFXVyfrVa%2FualogDVmf6h2t4Rdpifq8qTHsHFU3xpCz%2BT6%2FdGWKGQ42ZQfTaLnDM%0AjToAsmY0AyevkIbX6iZVtzGvanYpPcWW4X0RDPcpqfFNZk643xI4lsZ%2BY2Er9Yu5%0AS%2F8x0ly%2BtmmIokaE0wwbdUu740YTZjCesroYWiRg5zuQ2xfKxJoV5E%2BEh%2BtYwGDJ%0An6HfWhRgnudRRwvuJ45ztYVtKulKw8QQpd2STWrcQQDJaRWmnMooX%2FPATTjCBExB%0A9dkz38Druvk7IkHMtsIqlkAOQMdsX1d3Tov6BE2XDjIG0zFxLduJGbVwc%2F6rIc95%0AT055j36Ez0HrjxdpTGOOHxRqMK5m9flFbaxxtDnS7w77WqzW7HjFrD0VeTx2vnjj%0AGqchHEQpfDpFOzb8LTFhgYidyRNUflQY35WLOzLNV%2BpV3eQ3Jg11UFwelSNLqfQf%0AuFRGc%2BzcwkNjHh5yPvm9odR1BIfqJ6sKGPGbtPNXo7ERMRypWyRz0zi0twARAQAB%0AtChGZWRvcmEgRVBFTCAoNykgPGVwZWxAZmVkb3JhcHJvamVjdC5vcmc%2BiQI4BBMB%0AAgAiBQJSrmiEAhsPBgsJCAcDAgYVCAIJCgsEFgIDAQIeAQIXgAAKCRBqL66iNSxk%0A5cfGD%2F4spqpsTjtDM7qpytKLHKruZtvuWiqt5RfvT9ww9GUUFMZ4ZZGX4nUXg49q%0AixDLayWR8ddG%2Fs5kyOi3C0uX%2F6inzaYyRg%2BBh70brqKUK14F1BrrPi29eaKfG%2BGu%0AMFtXdBG2a7OtPmw3yuKmq9Epv6B0mP6E5KSdvSRSqJWtGcA6wRS%2FwDzXJENHp5re%0A9Ism3CYydpy0GLRA5wo4fPB5uLdUhLEUDvh2KK%2F%2FfMjja3o0L%2BSNz8N0aDZyn5Ax%0ACU9RB3EHcTecFgoy5umRj99BZrebR1NO%2B4gBrivIfdvD4fJNfNBHXwhSH9ACGCNv%0AHnXVjHQF9iHWApKkRIeh8Fr2n5dtfJEF7SEX8GbX7FbsWo29kXMrVgNqHNyDnfAB%0AVoPubgQdtJZJkVZAkaHrMu8AytwT62Q4eNqmJI1aWbZQNI5jWYqc6RKuCK6%2FF99q%0AthFT9gJO17%2ByRuL6Uv2%2FvgzVR1RGdwVLKwlUjGPAjYflpCQwWMAASxiv9uPyYPHc%0AErSrbRG0wjIfAR3vus1OSOx3xZHZpXFfmQTsDP7zVROLzV98R3JwFAxJ4%2FxqeON4%0AvCPFU6OsT3lWQ8w7il5ohY95wmujfr6lk89kEzJdOTzcn7DBbUru33CQMGKZ3Evt%0ARjsC7FDbL017qxS%2BZVA%2FHGkyfiu4cpgV8VUnbql5eAZ%2B1Ll6Dw%3D%3D%0A%3DhdPa%0A-----END%20PGP%20PUBLIC%20KEY%20BLOCK-----%0A"
        },
        "mode": 420
      }
    ]
  }
}`),
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
          "source": "data:,%5Bepel%5D%0Aname%3DExtra%20Packages%20for%20Enterprise%20Linux%207%20-%20%24basearch%0Ametalink%3Dhttps%3A%2F%2Fmirrors.fedoraproject.org%2Fmetalink%3Frepo%3Depel-7%26arch%3D%24basearch%0Afailovermethod%3Dpriority%0Aenabled%3D1%0Agpgcheck%3D1%0Agpgkey%3Dfile%3A%2F%2F%2Fetc%2Fpki%2Frpm-gpg%2FRPM-GPG-KEY-EPEL-7%0A"
        },
        "mode": 420
      },
      {
        "group": {
          "name": "root"
        },
        "path": "/etc/pki/rpm-gpg/RPM-GPG-KEY-EPEL-7",
        "user": {
          "name": "root"
        },
        "contents": {
          "source": "data:,-----BEGIN%20PGP%20PUBLIC%20KEY%20BLOCK-----%0AVersion%3A%20GnuPG%20v1.4.11%20(GNU%2FLinux)%0A%0AmQINBFKuaIQBEAC1UphXwMqCAarPUH%2FZsOFslabeTVO2pDk5YnO96f%2BrgZB7xArB%0AOSeQk7B90iqSJ85%2Fc72OAn4OXYvT63gfCeXpJs5M7emXkPsNQWWSju99lW%2BAqSNm%0AjYWhmRlLRGl0OO7gIwj776dIXvcMNFlzSPj00N2xAqjMbjlnV2n2abAE5gq6VpqP%0AvFXVyfrVa%2FualogDVmf6h2t4Rdpifq8qTHsHFU3xpCz%2BT6%2FdGWKGQ42ZQfTaLnDM%0AjToAsmY0AyevkIbX6iZVtzGvanYpPcWW4X0RDPcpqfFNZk643xI4lsZ%2BY2Er9Yu5%0AS%2F8x0ly%2BtmmIokaE0wwbdUu740YTZjCesroYWiRg5zuQ2xfKxJoV5E%2BEh%2BtYwGDJ%0An6HfWhRgnudRRwvuJ45ztYVtKulKw8QQpd2STWrcQQDJaRWmnMooX%2FPATTjCBExB%0A9dkz38Druvk7IkHMtsIqlkAOQMdsX1d3Tov6BE2XDjIG0zFxLduJGbVwc%2F6rIc95%0AT055j36Ez0HrjxdpTGOOHxRqMK5m9flFbaxxtDnS7w77WqzW7HjFrD0VeTx2vnjj%0AGqchHEQpfDpFOzb8LTFhgYidyRNUflQY35WLOzLNV%2BpV3eQ3Jg11UFwelSNLqfQf%0AuFRGc%2BzcwkNjHh5yPvm9odR1BIfqJ6sKGPGbtPNXo7ERMRypWyRz0zi0twARAQAB%0AtChGZWRvcmEgRVBFTCAoNykgPGVwZWxAZmVkb3JhcHJvamVjdC5vcmc%2BiQI4BBMB%0AAgAiBQJSrmiEAhsPBgsJCAcDAgYVCAIJCgsEFgIDAQIeAQIXgAAKCRBqL66iNSxk%0A5cfGD%2F4spqpsTjtDM7qpytKLHKruZtvuWiqt5RfvT9ww9GUUFMZ4ZZGX4nUXg49q%0AixDLayWR8ddG%2Fs5kyOi3C0uX%2F6inzaYyRg%2BBh70brqKUK14F1BrrPi29eaKfG%2BGu%0AMFtXdBG2a7OtPmw3yuKmq9Epv6B0mP6E5KSdvSRSqJWtGcA6wRS%2FwDzXJENHp5re%0A9Ism3CYydpy0GLRA5wo4fPB5uLdUhLEUDvh2KK%2F%2FfMjja3o0L%2BSNz8N0aDZyn5Ax%0ACU9RB3EHcTecFgoy5umRj99BZrebR1NO%2B4gBrivIfdvD4fJNfNBHXwhSH9ACGCNv%0AHnXVjHQF9iHWApKkRIeh8Fr2n5dtfJEF7SEX8GbX7FbsWo29kXMrVgNqHNyDnfAB%0AVoPubgQdtJZJkVZAkaHrMu8AytwT62Q4eNqmJI1aWbZQNI5jWYqc6RKuCK6%2FF99q%0AthFT9gJO17%2ByRuL6Uv2%2FvgzVR1RGdwVLKwlUjGPAjYflpCQwWMAASxiv9uPyYPHc%0AErSrbRG0wjIfAR3vus1OSOx3xZHZpXFfmQTsDP7zVROLzV98R3JwFAxJ4%2FxqeON4%0AvCPFU6OsT3lWQ8w7il5ohY95wmujfr6lk89kEzJdOTzcn7DBbUru33CQMGKZ3Evt%0ARjsC7FDbL017qxS%2BZVA%2FHGkyfiu4cpgV8VUnbql5eAZ%2B1Ll6Dw%3D%3D%0A%3DhdPa%0A-----END%20PGP%20PUBLIC%20KEY%20BLOCK-----%0A"
        },
        "mode": 420
      }
    ]
  }
}`),

		Distros: []string{"fcos", "rhcos"},
		Flags:   []register.Flag{register.RequiresInternetAccess}, // these need network to retrieve bits
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
// 'bcrypt' is available in EPEL and installs on Fedora 29 and RHEL 8
//
// NOTE: we could be churning on the package choice going forward as
// we need something that is a) small, b) has no dependencies, and c)
// can be installed on Fedora + RHEL from the EPEL repo that we are
// currently using.  We've already had to swap from `fpaste` to `bcrypt`
func rpmOstreeInstallUninstall(c cluster.TestCluster) {
	var installPkgName = "bcrypt"
	var installPkgBin string

	if c.Distribution() == "fcos" {
		installPkgBin = "/usr/bin/bcrypt"
	} else {
		installPkgBin = "/bin/bcrypt"
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
