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

package upgrade

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

const ostreeRepo = "/srv/ostree"

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/upgrade")

func init() {
	register.RegisterUpgradeTest(&register.Test{
		Run:         fcosUpgradeBasic,
		ClusterSize: 1,
		// if renaming this, also rename the command in kolet-httpd.service below
		Name:     "fcos.upgrade.basic",
		FailFast: true,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"httpd": register.CreateNativeFuncWrap(httpd),
		},
		Distros: []string{"fcos"},
		// This Ignition does a few things:
		// 1. bumps Zincati verbosity
		// 2. auto-runs httpd once kolet is scp'ed
		// 3. changes the zincati config to point to localhost:8080 so we'll be
		//    able to feed the update graph we want
		// 4. change the OSTree remote to localhost:8080
		// We could use file:/// to simplify things though using a URL at least
		// exercises the ostree/libcurl stack.
		// We use strings.Replace here because fmt.Sprintf would try to
		// interpret the percent signs and there's too many of them to be worth
		// escaping.
		UserDataV3: conf.Ignition(strings.Replace(`{
  "ignition": { "version": "3.0.0" },
  "systemd": {
    "units": [
      {
        "name": "zincati.service",
        "dropins": [{
          "name": "verbose.conf",
          "contents": "[Service]\nEnvironment=ZINCATI_VERBOSITY=\"-vvvv\""
        }]
      },
      {
        "name": "kolet-httpd.path",
        "enabled": true,
        "contents": "[Path]\nPathExists=/var/home/core/kolet\n[Install]\nWantedBy=multi-user.target"
      },
      {
        "name": "kolet-httpd.service",
        "contents": "[Service]\nExecStart=/var/home/core/kolet run fcos.upgrade.basic httpd -v\n[Install]\nWantedBy=multi-user.target"
      }
    ]
  },
  "storage": {
    "files": [
      {
        "path": "/etc/zincati/config.d/99-updates.toml",
        "contents": { "source": "data:,updates.enabled%20%3D%20true%0Acincinnati.base_url%3D%20%22http%3A%2F%2Flocalhost%3A8080%22%0A" },
        "mode": 420
      },
      {
        "path": "/etc/ostree/remotes.d/fedora.conf",
        "contents": { "source": "data:,%5Bremote%20%22fedora%22%5D%0Aurl%3Dhttp%3A%2F%2Flocalhost%3A8080%0Agpg-verify%3Dfalse%0A" },
        "overwrite": true,
        "mode": 420
      }
    ],
    "directories": [
      {
        "path": "OSTREE_REPO",
        "mode": 493,
        "user": {
          "name": "core"
        }
      }
    ]
  }
}`, "OSTREE_REPO", ostreeRepo, -1)),
	})
}

// upgradeFromPrevious verifies that the previous build is capable of upgrading
// to the current build and to another build
func fcosUpgradeBasic(c cluster.TestCluster) {
	m := c.Machines()[0]
	graph := new(Graph)

	c.Run("setup", func(c cluster.TestCluster) {
		// this is the only heavy-weight part, though remember this test is
		// optimized for qemu testing locally where this won't leave localhost at
		// all. cloud testing should mostly be a pipeline thing, where the infra
		// connection should be much faster
		ostreeTarPath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path)
		if err := cluster.DropFile(c.Machines(), ostreeTarPath); err != nil {
			c.Fatal(err)
		}

		// XXX: Note the '&& sync' here; this is to work around sysroot
		// remounting in libostree forcing a cache flush and blocking D-Bus.
		// Should drop this once we fix it more properly in {rpm-,}ostree.
		// https://github.com/coreos/coreos-assembler/issues/1301
		c.MustSSHf(m, "tar -xf %s -C %s && sync", kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path, ostreeRepo)

		graph.seedFromMachine(c, m)
		graph.addUpdate(c, m, kola.CosaBuild.Meta.OstreeVersion, kola.CosaBuild.Meta.OstreeCommit)
	})

	c.Run("upgrade-from-previous", func(c cluster.TestCluster) {
		waitForUpgradeToVersion(c, m, kola.CosaBuild.Meta.OstreeVersion)
	})

	// Now, synthesize an update and serve that -- this is similar to
	// `rpmostree.upgrade-rollback`, but the major difference here is that the
	// starting disk is the previous release (and also, we're doing this via
	// Zincati & HTTP). Essentially, this sanity-checks that old starting state
	// + new content set can update.
	c.Run("upgrade-from-current", func(c cluster.TestCluster) {
		newVersion := kola.CosaBuild.Meta.OstreeVersion + ".kola"
		newCommit := c.MustSSHf(m, "ostree commit --repo %s -b %s --tree ref=%s --add-metadata-string version=%s",
			ostreeRepo, kola.CosaBuild.Meta.BuildRef, kola.CosaBuild.Meta.OstreeCommit, newVersion)

		graph.addUpdate(c, m, newVersion, string(newCommit))

		waitForUpgradeToVersion(c, m, newVersion)
	})
}

// Should dedupe this with fedora-coreos-cincinnati -- we just handle the
// bare minimum here. One question here is: why not use Cincinnati itself for
// this? We could do this, though it'd somewhat muddle the focus of these tests
// and make setup more complex.
type Graph struct {
	Nodes []Node   `json:"nodes"`
	Edges [][2]int `json:"edges,omitempty"`
}

type Node struct {
	Version  string            `json:"version"`
	Metadata map[string]string `json:"metadata"`
	Payload  string            `json:"payload"`
}

func (g *Graph) seedFromMachine(c cluster.TestCluster, m platform.Machine) {
	d, err := util.GetBootedDeployment(c, m)
	if err != nil {
		c.Fatal(err)
	}

	g.Nodes = []Node{
		{
			Version: d.Version,
			Payload: d.Checksum,
			Metadata: map[string]string{
				"org.fedoraproject.coreos.releases.age_index": "0",
				"org.fedoraproject.coreos.scheme":             "checksum",
			},
		},
	}

	g.sync(c, m)
}

func (g *Graph) addUpdate(c cluster.TestCluster, m platform.Machine, version, payload string) {
	i := len(g.Nodes)

	g.Nodes = append(g.Nodes, Node{
		Version: version,
		Payload: payload,
		Metadata: map[string]string{
			"org.fedoraproject.coreos.releases.age_index": strconv.Itoa(i),
			"org.fedoraproject.coreos.scheme":             "checksum",
		},
	})

	g.Edges = append(g.Edges, [2]int{i - 1, i})

	g.sync(c, m)
}

func (g *Graph) sync(c cluster.TestCluster, m platform.Machine) {
	b, err := json.Marshal(g)
	if err != nil {
		c.Fatalf("failed to marshal graph: %v")
	}

	if err := platform.InstallFile(bytes.NewReader(b), m, "graph.json"); err != nil {
		c.Fatalf("failed to update graph.json: %v", err)
	}
}

func waitForUpgradeToVersion(c cluster.TestCluster, m platform.Machine, version string) {
	oldBootId, err := platform.GetMachineBootId(m)
	if err != nil {
		c.Fatal(err)
	}

	// XXX: patch zincati to have faster refresh rate
	// https://github.com/coreos/zincati/issues/203
	c.MustSSH(m, "sudo systemctl restart zincati.service")

	if err := m.WaitForReboot(120*time.Second, oldBootId); err != nil {
		c.Fatalf("failed waiting for machine reboot: %v", err)
	}

	d, err := util.GetBootedDeployment(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if d.Version != version {
		c.Fatalf("expected reboot into version %s, but got version %s", version, d.Version)
	}
}

func httpd() error {
	http.Handle("/", http.FileServer(http.Dir(ostreeRepo)))
	http.HandleFunc("/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "/var/home/core/graph.json")
	})
	plog.Info("Starting server")
	return http.ListenAndServe("localhost:8080", nil)
}
