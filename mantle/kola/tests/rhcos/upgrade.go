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

package rhcos

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterUpgradeTest(&register.Test{
		Run:         rhcosUpgrade,
		ClusterSize: 1,
		// if renaming this, also rename the command in kolet-httpd.service below
		Name:                 "rhcos.upgrade.luks",
		FailFast:             true,
		Tags:                 []string{"upgrade"},
		Distros:              []string{"rhcos"},
		ExcludeArchitectures: []string{"s390x", "aarch64"}, // no TPM backend support for s390x and upgrade test not valid for aarch64
		UserData: conf.Ignition(`{
			"ignition": {
				"version": "3.0.0"
			},
			"storage": {
				"files": [
					{
						"path": "/etc/clevis.json",
						"contents": {
							"source": "data:text/plain;base64,e30K"
						},
						"mode": 420
					}
				]
			}
		}`),
	})

	register.RegisterUpgradeTest(&register.Test{
		Run:         rhcosUpgradeBasic,
		ClusterSize: 1,
		// if renaming this, also rename the command in kolet-httpd.service below
		Name:                 "rhcos.upgrade.basic",
		FailFast:             true,
		Tags:                 []string{"upgrade"},
		Distros:              []string{"rhcos"},
		ExcludeArchitectures: []string{"aarch64"}, //upgrade test not valid for aarch64
		UserData: conf.Ignition(`{
                        "ignition": {
                                "version": "3.0.0"
                        }
                }`),
	})

}

// Ensure that we can still boot into a system with LUKS rootfs after
// an upgrade.
func rhcosUpgrade(c cluster.TestCluster) {
	m := c.Machines()[0]
	ostreeCommit := kola.CosaBuild.Meta.OstreeCommit
	ostreeTarName := kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path
	// See tests/upgrade/basic.go for some more information on this; in the future
	// we should optimize this to use virtio-fs for qemu.
	c.Run("setup", func(c cluster.TestCluster) {
		var tempTar string
		defer func() {
			if tempTar != "" {
				os.Remove(tempTar)
			}
		}()

		var ostreeTarPath string
		if strings.HasSuffix(ostreeTarName, ".ociarchive") {
			// For now, downgrade this to a tarball until rpm-ostree on RHCOS8 gains support for oci natively.
			outputOstreeTarName := "tmp/ " + strings.Replace(ostreeTarName, ".ociarchive", ".tar", 1)
			// We also right now need a dance to write to a bare-user repo until
			// the object writing path can directly write to archive repos.
			cmd := exec.Command("/bin/bash", "-c", fmt.Sprintf(`set -euo pipefail;
				tarname="%s"
				outputname="%s"
				commit="%s"
				ostree --repo=tmp/repo-cache init --mode=bare-user
				rpm-ostree ex-container import --repo=tmp/repo ostree-unverified-image:oci-archive:$tarname:latest
				ostree --repo=tmp/repo pull-local tmp/repo-cache "$commit"
				tar -cf "$outputname" -C tmp/repo .
				rm tmp/repo-cache -rf
			 `, filepath.Join(kola.CosaBuild.Dir, ostreeTarName), outputOstreeTarName, ostreeCommit))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				c.Fatal(err)
			}
			tempTar = outputOstreeTarName
			ostreeTarPath = outputOstreeTarName
			ostreeTarName = filepath.Base(ostreeTarPath)
		} else {
			ostreeTarPath = filepath.Join(kola.CosaBuild.Dir, ostreeTarName)
		}
		if err := cluster.DropFile(c.Machines(), ostreeTarPath); err != nil {
			c.Fatal(err)
		}

		// XXX: Note the '&& sync' here; this is to work around sysroot
		// remounting in libostree forcing a cache flush and blocking D-Bus.
		// Should drop this once we fix it more properly in {rpm-,}ostree.
		// https://github.com/coreos/coreos-assembler/issues/1301
		// Also we should really add a streaming import for this
		c.RunCmdSyncf(m, "sudo tar -xf %s -C /var/srv && sudo rm %s", ostreeTarName, ostreeTarName)
		c.RunCmdSyncf(m, "sudo ostree --repo=/sysroot/ostree/repo pull-local /var/srv %s && sudo rm -rf /var/srv/* && sudo sync", ostreeCommit)
	})

	c.Run("upgrade-from-previous", func(c cluster.TestCluster) {
		c.RunCmdSyncf(m, "sudo rpm-ostree rebase :%s", ostreeCommit)
		err := m.Reboot()
		if err != nil {
			c.Fatalf("Failed to reboot machine: %v", err)
		}
	})

	c.Run("verify", func(c cluster.TestCluster) {
		d, err := util.GetBootedDeployment(c, m)
		if err != nil {
			c.Fatal(err)
		}
		if d.Checksum != kola.CosaBuild.Meta.OstreeCommit {
			c.Fatalf("Got booted checksum=%s expected=%s", d.Checksum, kola.CosaBuild.Meta.OstreeCommit)
		}
		// And we should also like systemctl --failed here and stuff
	})
}

// A basic non-LUKS upgrade test which will test the migration of rootfs from crypt_rootfs to regular root
func rhcosUpgradeBasic(c cluster.TestCluster) {
	m := c.Machines()[0]
	rhcosUpgrade(c)
	c.Run("rootfs-migration", func(c cluster.TestCluster) {
		err := m.Reboot()
		if err != nil {
			c.Fatalf("Failed to reboot machine: %v", err)
		}
	})

	c.Run("verify-rootfs-migration", func(c cluster.TestCluster) {
		c.RunCmdSync(m, "ls /dev/disk/by-label/root")
	})
}
