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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cosa "github.com/coreos/coreos-assembler/pkg/builds"
	coreosarch "github.com/coreos/stream-metadata-go/arch"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/kola/tests/util"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	installer "github.com/coreos/coreos-assembler/mantle/util"
	"github.com/coreos/go-semver/semver"
)

func init() {
	register.RegisterUpgradeTest(&register.Test{
		Run:         rhcosUpgrade,
		ClusterSize: 1,
		// if renaming this, also rename the command in kolet-httpd.service below
		Name:                 "rhcos.upgrade.luks",
		Description:          "Verify that rhcos supports upgrading with LUKS.",
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
		Description:          "Verify that rhcos supports upgrading.",
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

	register.RegisterTest(&register.Test{
		Run:                  rhcosUpgradeFromOcpRhcos,
		ClusterSize:          0,
		Name:                 "rhcos.upgrade.from-ocp-rhcos",
		Description:          "Verify upgrading from the latest RHCOS released for OCP works.",
		FailFast:             true,
		Tags:                 []string{"upgrade", kola.NeedsInternetTag},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"},
		UserData: conf.Ignition(`{
                        "ignition": {
                                "version": "3.0.0"
                        }
                }`),
	})
}

func setup(c cluster.TestCluster) {
	m := c.Machines()[0]
	ostreeCommit := kola.CosaBuild.Meta.OstreeCommit
	ostreeTarName := kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path
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
		// TODO change this to "ostree container" once we have at least rpm-ostree v2022.1
		cmd := exec.Command("/bin/bash", "-c", fmt.Sprintf(`set -euo pipefail;
			tarname="%s"
			outputname="%s"
			commit="%s"
			ostree --repo=tmp/repo-cache init --mode=bare-user
			ostree container unencapsulate --repo=tmp/repo ostree-unverified-image:oci-archive:$tarname:latest
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
}

// Ensure that we can still boot into a system with LUKS rootfs after
// an upgrade.
func rhcosUpgrade(c cluster.TestCluster) {
	m := c.Machines()[0]
	ostreeCommit := kola.CosaBuild.Meta.OstreeCommit
	// See tests/upgrade/basic.go for some more information on this; in the future
	// we should optimize this to use virtio-fs for qemu.
	c.Run("setup", setup)

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
		if d.Checksum != ostreeCommit {
			c.Fatalf("Got booted checksum=%s expected=%s", d.Checksum, ostreeCommit)
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

// This test boots the RHCOS version for the latest OCP release for a given
// stream and upgrades to the current build.  It also checks that there are
// no downgraded packages
func rhcosUpgradeFromOcpRhcos(c cluster.TestCluster) {
	var m platform.Machine
	options := platform.QemuMachineOptions{}
	ignition := conf.Ignition(`{
		"ignition": {
			"version": "3.0.0"
		}
	}`)

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		ostreeCommit := kola.CosaBuild.Meta.OstreeCommit
		temp := os.TempDir()
		rhcosQcow2, err := downloadLatestReleasedRHCOS(temp)
		if err != nil {
			c.Fatal(err)
		}

		// skip on unreleased OCP versions
		if rhcosQcow2 == "" {
			c.SkipNow()
		}
		defer os.Remove(rhcosQcow2)

		options.OverrideBackingFile = rhcosQcow2
		m, err = pc.NewMachineWithQemuOptions(ignition, options)
		if err != nil {
			c.Fatal(err)
		}

		// See tests/upgrade/basic.go for some more information on this; in the future
		// we should optimize this to use virtio-fs for qemu.
		c.Run("setup", setup)
		c.Run("upgrade", func(c cluster.TestCluster) {
			c.RunCmdSyncf(m, "sudo rpm-ostree rebase :%s", ostreeCommit)
			err := m.Reboot()
			if err != nil {
				c.Fatalf("Failed to reboot machine: %v", err)
			}
		})
		c.Run("verify-upgrade", func(c cluster.TestCluster) {
			d, err := util.GetBootedDeployment(c, m)
			if err != nil {
				c.Fatal(err)
			}
			if d.Checksum != ostreeCommit {
				c.Fatalf("Got booted checksum=%s expected=%s", d.Checksum, ostreeCommit)
			}
		})
		c.Run("verify-no-pkg-downgrades", func(c cluster.TestCluster) {
			outputBuffer := c.MustSSH(m, "rpm-ostree db diff")
			output := string(outputBuffer)
			if strings.Contains(output, "Downgraded") {
				c.Fatalf("Downgraded packages found:\n%s", output)
			}
		})
	default:
		c.Fatal("Platform unsupported")
	}

}

// getJSON retrieves a JSON URL and unmarshals it into an interface
func getJson(url string, target interface{}) error {

	myClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error with new request")
	}
	req.Header.Set("Accept", "application/json")
	r, err := myClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

// Downloads the latest RHCOS from an OCP stream and decompresses it.
// Returns the path to the decompressed file
func downloadLatestReleasedRHCOS(target string) (string, error) {
	buildID := kola.CosaBuild.Meta.BuildID
	ocpVersion := strings.Split(buildID, ".")[0]
	rhelVersion := strings.Split(buildID, ".")[1]
	ocpVersionF := fmt.Sprintf("%s.%s", ocpVersion[:1], ocpVersion[1:])
	// The stream name isn't anywhere in the build so we can only infer
	// the RHEL version from the RHCOS version string. This will break
	// on three digit versions like 10.1 or 9.10.
	rhelVersionF := fmt.Sprintf("%s.%s", rhelVersion[:1], rhelVersion[1:])
	channel := "fast-" + ocpVersionF

	type Release struct {
		Version string `json:"version"`
		Payload string `json:"payload"`
	}

	type Graph struct {
		Nodes []Release `json:"nodes"`
		Edges [][]int   `json:"edges"`
	}

	type MachineOS struct {
		Version     string `json:"Version"`
		DisplayName string `json:"DisplayName"`
	}

	type DisplayVersions struct {
		MachineOS MachineOS `json:"machine-os"`
	}

	type OcpRelease struct {
		DisplayVersions DisplayVersions `json:"displayVersions"`
	}

	graph := &Graph{}
	graphUrl := fmt.Sprintf("https://api.openshift.com/api/upgrades_info/v1/graph?channel=%s", channel)
	if err := getJson(graphUrl, &graph); err != nil {
		return "", err
	}

	// no-op on unreleased OCP versions
	if len(graph.Nodes) == 0 {
		return "", nil
	}

	// Find the latest OCP release by looking at the edges and comparing it to
	// the nodes. Edges define updates as [from release index, to release index]
	// so the node that doesn't show up in a from release index is the latest.
	fromEdge := []int{}
	releaseIndex := []int{}
	for i := 0; i < len(graph.Nodes); i++ {
		releaseIndex = append(releaseIndex, i)
	}

	for _, v := range graph.Edges {
		fromEdge = append(fromEdge, v[0])
	}

	unique := func(intSlice []int) []int {
		keys := make(map[int]bool)
		list := []int{}
		for _, entry := range intSlice {
			if _, value := keys[entry]; !value {
				keys[entry] = true
				list = append(list, entry)
			}
		}
		return list
	}(fromEdge)

	difference := func(a, b []int) (diff []int) {
		m := make(map[int]bool)
		for _, item := range b {
			m[item] = true
		}

		for _, item := range a {
			if _, ok := m[item]; !ok {
				diff = append(diff, item)
			}
		}
		return
	}(releaseIndex, unique)

	// In cases where there is a blocked upgrade to a new Y-stream there can be
	// two nodes that don't have an edge to upgrade to. This is generally the
	// latest 4.Y-1.Z and the latest 4.Y.Z. Choose the latest 4.Y.Z
	if len(difference) < 1 {
		return "", errors.New("Could not find the latest release")
	}
	latest := difference[0]
	if len(difference) > 1 {
		latestVersion := semver.New(graph.Nodes[latest].Version)
		for _, v := range difference {
			currentVersion := semver.New(graph.Nodes[v].Version)
			if latestVersion.LessThan(*currentVersion) {
				latest = v
				latestVersion = currentVersion
			}
		}
	}

	var ocpRelease *OcpRelease
	latestOcpPayload := graph.Nodes[latest].Payload
	// oc should be included in cosa since https://github.com/coreos/coreos-assembler/pull/2777
	cmd := exec.Command("/usr/bin/oc", "adm", "release", "info", latestOcpPayload, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(output, &ocpRelease); err != nil {
		return "", err
	}

	var latestOcpRhcosBuild *cosa.Build
	rhcosVersion := ocpRelease.DisplayVersions.MachineOS.Version
	latestBaseUrl := fmt.Sprintf("https://rhcos.mirror.openshift.com/art/storage/prod/streams/%s-%s/builds/%s/%s",
		ocpVersionF,
		rhelVersionF,
		rhcosVersion,
		coreosarch.CurrentRpmArch())
	latestRhcosBuildMetaUrl := fmt.Sprintf("%s/meta.json", latestBaseUrl)
	if err := getJson(latestRhcosBuildMetaUrl, &latestOcpRhcosBuild); err != nil {
		// Try the <ocp version> stream; ideally we'd only do this if the error
		// was 403 denied (which is really a 404), but meh this is temporary
		// anyway.
		latestBaseUrl = fmt.Sprintf("https://rhcos.mirror.openshift.com/art/storage/prod/streams/%s/builds/%s/%s",
			ocpVersionF,
			rhcosVersion,
			coreosarch.CurrentRpmArch())
		latestRhcosBuildMetaUrl = fmt.Sprintf("%s/meta.json", latestBaseUrl)
		if err := getJson(latestRhcosBuildMetaUrl, &latestOcpRhcosBuild); err != nil {
			return "", err
		}
	}

	latestRhcosQcow2 := latestOcpRhcosBuild.BuildArtifacts.Qemu.Path
	latestRhcosQcow2Url := fmt.Sprintf("%s/%s", latestBaseUrl, latestRhcosQcow2)
	rhcosQcow2GzPath := fmt.Sprintf("%s/%s", target, latestRhcosQcow2)
	rhcosQcow2Path, err := installer.DownloadImageAndDecompress(latestRhcosQcow2Url,
		rhcosQcow2GzPath,
		true)
	if err != nil {
		return "", err
	}
	defer os.Remove(rhcosQcow2GzPath)

	return rhcosQcow2Path, nil
}
