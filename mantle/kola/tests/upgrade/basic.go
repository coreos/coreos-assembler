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

const workdir = "/var/srv/upgrade"
const ostreeRepo = workdir + "/repo"
const zincatiMetricsSocket = "/run/zincati/public/metrics.promsock"

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
		Tags:    []string{"upgrade"},
		Distros: []string{"fcos"},
		// This Ignition does a few things:
		// 1. bumps Zincati verbosity
		// 2. auto-runs httpd once kolet is scp'ed
		// 3. changes the Zincati config to point to localhost:8080 so we'll be
		//    able to feed the update graph we want
		// 4. always start with Zincati updates disabled so we can finish
		//    setting it up here before enabling it again without risking race
		//    conditions
		// 5. change the OSTree remote to localhost:8080
		// We could use file:/// to simplify things though using a URL at least
		// exercises the ostree/libcurl stack.
		// We use strings.Replace here because fmt.Sprintf would try to
		// interpret the percent signs and there's too many of them to be worth
		// escaping.
		UserData: conf.Ignition(strings.Replace(`{
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
        "path": "/etc/zincati/config.d/99-cincinnati-url.toml",
        "contents": { "source": "data:,cincinnati.base_url%3D%20%22http%3A%2F%2Flocalhost%3A8080%22%0A" },
        "mode": 420
      },
      {
        "path": "/etc/zincati/config.d/99-updates.toml",
        "contents": { "source": "data:,updates.enabled%20%3D%20true%0A" },
        "mode": 420
      },
      {
        "path": "/etc/zincati/config.d/99-agent-timing-speedup.toml",
        "contents": { "source": "data:,agent.timing.steady_interval_secs%20%3D%2020%0A" },
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
        "path": "WORKDIR",
        "mode": 493,
        "user": {
          "name": "core"
        }
      }
    ]
  }
}`, "WORKDIR", workdir, -1)),
	})
}

// upgradeFromPrevious verifies that the previous build is capable of upgrading
// to the current build and to another build
func fcosUpgradeBasic(c cluster.TestCluster) {
	m := c.Machines()[0]
	graph := new(Graph)

	c.Run("setup", func(c cluster.TestCluster) {
		ostreeblob := kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path
		ostreeref := kola.CosaBuild.Meta.BuildRef
		// this is the only heavy-weight part, though remember this test is
		// optimized for qemu testing locally where this won't leave localhost at
		// all. cloud testing should mostly be a pipeline thing, where the infra
		// connection should be much faster
		ostreeTarPath := filepath.Join(kola.CosaBuild.Dir, ostreeblob)
		if err := cluster.DropFile(c.Machines(), ostreeTarPath); err != nil {
			c.Fatal(err)
		}

		// Keep any changes around here in sync with tests/rhcos/upgrade.go too!

		// See https://github.com/coreos/fedora-coreos-tracker/issues/812
		if strings.HasSuffix(ostreeblob, ".ociarchive") {
			tmprepo := workdir + "/repo-bare"
			// TODO: https://github.com/ostreedev/ostree-rs-ext/issues/34
			c.RunCmdSyncf(m, "ostree --repo=%s init --mode=bare-user", tmprepo)
			c.RunCmdSyncf(m, "ostree container import --repo=%s --write-ref %s ostree-unverified-image:oci-archive:%s:latest", tmprepo, ostreeref, ostreeblob)
			c.RunCmdSyncf(m, "ostree --repo=%s init --mode=archive", ostreeRepo)
			c.RunCmdSyncf(m, "ostree --repo=%s pull-local %s %s", ostreeRepo, tmprepo, ostreeref)
		} else {
			c.RunCmdSyncf(m, "mkdir -p %s && tar -xf %s -C %s", ostreeRepo, ostreeblob, ostreeRepo)
		}

		// XXX: This is to work around sysroot
		// remounting in libostree forcing a cache flush and blocking D-Bus.
		// Should drop this once we fix it more properly in {rpm-,}ostree.
		// https://github.com/coreos/coreos-assembler/issues/1301
		c.RunCmdSync(m, "time sudo sync")

		// disable zincati; from now on, we'll start it manually whenenever we
		// want to upgrade via Zincati
		c.RunCmdSync(m, "sudo systemctl disable --now --quiet zincati.service")
		c.RunCmdSync(m, "sudo rm /etc/zincati/config.d/99-updates.toml")
		// delete what mantle adds (XXX: should just opt out of this upfront)
		c.RunCmdSync(m, "sudo rm /etc/zincati/config.d/90-disable-auto-updates.toml")

	})

	c.Run("upgrade-from-previous", func(c cluster.TestCluster) {
		// We need to check now whether this is a within-stream update or a
		// cross-stream rebase.
		d, err := util.GetBootedDeployment(c, m)
		if err != nil {
			c.Fatal(err)
		}
		if strings.HasSuffix(d.Origin, ":"+kola.CosaBuild.Meta.BuildRef) {
			// same stream; let's use Zincati
			graph.seedFromMachine(c, m)
			graph.addUpdate(c, m, kola.CosaBuild.Meta.OstreeVersion, kola.CosaBuild.Meta.OstreeCommit)
			waitForUpgradeToVersion(c, m, kola.CosaBuild.Meta.OstreeVersion)
		} else {
			rebaseToStream(c, m, kola.CosaBuild.Meta.BuildRef, kola.CosaBuild.Meta.OstreeVersion)
			// and from now on we can use Zincati, so seed the graph with the new node
			graph.seedFromMachine(c, m)
		}
	})

	// Now, synthesize an update and serve that -- this is similar to
	// `rpmostree.upgrade-rollback`, but the major difference here is that the
	// starting disk is the previous release (and also, we're doing this via
	// Zincati & HTTP). Essentially, this sanity-checks that old starting state
	// + new content set can update.
	c.Run("upgrade-from-current", func(c cluster.TestCluster) {
		newVersion := kola.CosaBuild.Meta.OstreeVersion + ".kola"
		ostree_command := "ostree commit --repo %s -b %s --tree ref=%s --add-metadata-string version=%s " +
			"--keep-metadata='fedora-coreos.stream' --keep-metadata='coreos-assembler.basearch' --parent=%s"
		newCommit := c.MustSSHf(m,
			ostree_command,
			ostreeRepo, kola.CosaBuild.Meta.BuildRef, kola.CosaBuild.Meta.OstreeCommit, newVersion, kola.CosaBuild.Meta.OstreeCommit)

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

// XXX: consider making this distinction part of FCOS itself?
func onProdStream(c cluster.TestCluster, d *util.RpmOstreeDeployment) bool {
	switch d.BaseCommitMeta.FedoraCoreOSStream {
	case "":
		c.Fatalf("missing fedora-coreos.stream metadata key")
	case "stable", "testing", "next":
		return true
	}

	return false
}

func isDevBuild(c cluster.TestCluster, d *util.RpmOstreeDeployment) bool {
	return strings.Contains(d.Version, "dev")
}

// On production streams, the default should already be to have updates turned
// on, so we shouldn't have to delete anything. On developer and/or
// non-production streams, we have to delete other knobs.
func undoZincatiDisablement(c cluster.TestCluster, m platform.Machine) {
	d, err := util.GetBootedDeployment(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if !onProdStream(c, d) {
		c.RunCmdSync(m, "sudo rm -f /etc/zincati/config.d/90-disable-on-non-production-stream.toml")
	}

	if isDevBuild(c, d) {
		c.RunCmdSync(m, "sudo rm -f /etc/zincati/config.d/95-disable-on-dev.toml")
	}
}

func (g *Graph) sync(c cluster.TestCluster, m platform.Machine) {
	b, err := json.Marshal(g)
	if err != nil {
		c.Fatalf("failed to marshal graph: %v", err)
	}

	if err := platform.InstallFile(bytes.NewReader(b), m, "graph.json"); err != nil {
		c.Fatalf("failed to update graph.json: %v", err)
	}
}

func runFnAndWaitForRebootIntoVersion(c cluster.TestCluster, m platform.Machine, version string, fn func()) {
	oldBootId, err := platform.GetMachineBootId(m)
	if err != nil {
		c.Fatal(err)
	}

	fn()

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

func waitForUpgradeToVersion(c cluster.TestCluster, m platform.Machine, version string) {
	// we have to do this every time in case e.g. we've just rebased from an
	// official pipeline build to a developer build
	undoZincatiDisablement(c, m)

	runFnAndWaitForRebootIntoVersion(c, m, version, func() {
		// XXX: update to use https://github.com/coreos/zincati/issues/203
		c.RunCmdSync(m, "sudo systemctl start zincati.service")
	})
}

func rebaseToStream(c cluster.TestCluster, m platform.Machine, ref, version string) {
	runFnAndWaitForRebootIntoVersion(c, m, version, func() {
		c.RunCmdSyncf(m, "sudo systemctl stop zincati")
		// we use systemd-run here so that we can test the --reboot path
		// without having SSH not exit cleanly, which would cause an error
		c.RunCmdSyncf(m, "sudo systemd-run rpm-ostree rebase --reboot %s", ref)
	})
}

func httpd() error {
	http.Handle("/", http.FileServer(http.Dir(ostreeRepo)))
	http.HandleFunc("/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "/var/home/core/graph.json")
	})
	plog.Info("Starting server")
	return http.ListenAndServe("localhost:8080", nil)
}
