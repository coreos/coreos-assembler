// Copyright 2017 CoreOS, Inc.
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

package docker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coreos/go-semver/semver"
	ignition "github.com/coreos/ignition/config/v2_1/types"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/torcx"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Run:              dockerTorcxManifestPkgs,
		ClusterSize:      0,
		Name:             "docker.torcx-manifest-pkgs",
		ExcludePlatforms: []string{"qemu"}, // Downloads torcx packages
		// the first version torcx manifests were shipped
		MinVersion: semver.Version{Major: 1520},
	})
}

func dockerTorcxManifestPkgs(c cluster.TestCluster) {
	if kola.TorcxManifest == nil {
		c.Skip("no torcx manifest provided")
		return
	}

	var dockerPkgs *torcx.Package
	for _, pkg := range kola.TorcxManifest.Packages {
		pkg := pkg
		if pkg.Name == "docker" {
			dockerPkgs = &pkg
			break
		}
	}
	if dockerPkgs == nil {
		c.Fatalf("torcx manifest provided, but didn't include docker packages: %+v", kola.TorcxManifest)
	}

	// Generate an ignition config that downloads all of the docker torcx packages referenced
	ignitionConfig := ignition.Config{
		Ignition: ignition.Ignition{
			Version: "2.1.0",
		},
		Storage: ignition.Storage{
			Files: []ignition.File{},
		},
	}

	for _, version := range dockerPkgs.Versions {
		version := version
		var url string
		for _, loc := range version.Locations {
			if loc.URL != nil {
				url = *loc.URL
			}
		}
		if url == "" {
			c.Fatalf("not all docker versions had a remote location available: %+v", kola.TorcxManifest)
		}

		ignitionConfig.Storage.Files = append(ignitionConfig.Storage.Files, ignition.File{
			Node: ignition.Node{
				Filesystem: "root",
				Path:       fmt.Sprintf("/var/lib/torcx/store/docker:%s.torcx.tgz", version.Version),
			},
			FileEmbedded1: ignition.FileEmbedded1{
				Contents: ignition.FileContents{
					Source: url,
					Verification: ignition.Verification{
						Hash: &version.Hash,
					},
				},
			},
		})
	}

	ignitionBytes, err := json.Marshal(ignitionConfig)
	if err != nil {
		c.Fatalf("marshal err: %v", err)
	}

	m, err := c.NewMachine(conf.Ignition(string(ignitionBytes)))
	if err != nil {
		c.Fatalf("could not boot machine: %v", err)
	}

	// Make sure the default torcx config was fine
	if _, err := c.SSH(m, `docker version`); err != nil {
		c.Fatalf("could not run docker: %v", err)
	}

	// And now swap in a profile for each package and make sure it works
	for _, version := range dockerPkgs.Versions {
		version := version.Version
		c.Run("torcx-pkg-"+version, func(c cluster.TestCluster) {
			testPackageVersion(m, c, version)
		})
	}
}

func testPackageVersion(m platform.Machine, c cluster.TestCluster, version string) {
	c.Run("install-torcx-profile", func(c cluster.TestCluster) {
		_, err := c.SSH(m, fmt.Sprintf(`sudo tee /etc/torcx/profiles/docker.json <<EOF
{
  "kind": "profile-manifest-v0",
  "value": {
    "images": [
      {
        "name": "docker",
        "reference": "%s"
      }
    ]
  }
}
EOF
echo "docker" | sudo tee /etc/torcx/next-profile
`, version))
		if err != nil {
			c.Fatalf("could not set profile: %v", err)
		}

		if err := m.Reboot(); err != nil {
			c.Fatalf("could not reboot: %v", err)
		}
		if _, err := c.SSH(m, `sudo rm -rf /var/lib/docker`); err != nil {
			c.Fatalf("could not wipe /var/lib/docker: %v", err)
		}
		currentVersion := getTorcxDockerReference(c, m)
		if currentVersion != version {
			c.Fatalf("expected version to be %s, was %s", version, currentVersion)
		}

		serverVersion := getDockerServerVersion(c, m)
		// torcx packages have truncated docker versions, e.g. 1.12.6 has a torcx
		// package of 1.12
		if !strings.HasPrefix(serverVersion, version) {
			c.Fatalf("expected a version similar to %v, was %v", version, serverVersion)
		}

	})

	dockerBaseTests(c)
}

func getTorcxDockerReference(c cluster.TestCluster, m platform.Machine) string {
	ver, err := c.SSH(m, `jq -r '.value.images[] | select(.name == "docker").reference' /run/torcx/profile.json`)
	if err != nil {
		c.Fatalf("could not get current docker ref: %v", err)
	}
	return string(ver)
}

func getDockerServerVersion(c cluster.TestCluster, m platform.Machine) string {
	ver, err := c.SSH(m, `curl -s --unix-socket /var/run/docker.sock http://docker/v1.24/info | jq -r '.ServerVersion'`)
	if err != nil {
		c.Fatalf("could not get docker version: %v", err)
	}
	return string(ver)
}
