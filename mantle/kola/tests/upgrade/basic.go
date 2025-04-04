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
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/kola/tests/util"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const workdir = "/var/srv/upgrade"
const ostreeRepo = workdir + "/repo"

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "kola/tests/upgrade")

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
		// 4. changes the Zincati config to have a 99-updates-enabled.toml config
		//    that overrides any previous config that would have disabled them like
		//    the following that are dropped in various scenarios:
		//      - 90-disable-auto-updates.toml
		//      - 90-disable-on-non-production-stream.toml
		//      - 95-disable-on-dev.toml
		// 5. disables zincati.service in Ignition so we can finish setting it up here
		//    before starting it again without risking race conditions
		// 6. change the OSTree remote to localhost:8080
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
        "enabled": false,
        "dropins": [{
          "name": "verbose.conf",
          "contents": "[Service]\nEnvironment=ZINCATI_VERBOSITY=\"-vvvv\""
        }]
      },
      {
        "name": "kolet-httpd.path",
        "enabled": true,
        "contents": "[Path]\nPathExists=/usr/local/bin/kolet\n[Install]\nWantedBy=multi-user.target"
      },
      {
        "name": "kolet-httpd.service",
        "contents": "[Service]\nExecStart=/usr/local/bin/kolet run fcos.upgrade.basic httpd -v\n[Install]\nWantedBy=multi-user.target"
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
        "path": "/etc/zincati/config.d/99-updates-enabled.toml",
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

	containerImageFilename := kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path

	rpmostreeStatus, err := util.GetRpmOstreeStatus(c, m)
	if err != nil {
		c.Fatal(err)
	}
	booted, err := rpmostreeStatus.GetBootedDeployment()
	if err != nil {
		c.Fatal(err)
	}
	usingContainer := booted.ContainerImageReference != ""
	sourceContainerRef := fmt.Sprintf("ostree-unverified-image:oci-archive:%s:latest", containerImageFilename)

	c.Run("setup", func(c cluster.TestCluster) {
		ostreeref := kola.CosaBuild.Meta.BuildRef
		// this is the only heavy-weight part, though remember this test is
		// optimized for qemu testing locally where this won't leave localhost at
		// all. cloud testing should mostly be a pipeline thing, where the infra
		// connection should be much faster
		ostreeTarPath := filepath.Join(kola.CosaBuild.Dir, containerImageFilename)
		if err := cluster.DropFile(c.Machines(), ostreeTarPath); err != nil {
			c.Fatal(err)
		}

		// Keep any changes around here in sync with tests/rhcos/upgrade.go too!

		// See https://github.com/coreos/fedora-coreos-tracker/issues/812
		if usingContainer {
			// In the container path we'll pass this file directly, so put it outside
			// of the user's home directory so the systemd service can find it.
			c.RunCmdSyncf(m, "sudo mv %s /var/tmp/%s", containerImageFilename, containerImageFilename)
			sourceContainerRef = fmt.Sprintf("ostree-unverified-image:oci-archive:/var/tmp/%s:latest", containerImageFilename)
		} else {
			tmprepo := workdir + "/repo-bare"
			// TODO: https://github.com/ostreedev/ostree-rs-ext/issues/34
			c.RunCmdSyncf(m, "ostree --repo=%s init --mode=bare-user", tmprepo)
			c.RunCmdSyncf(m, "ostree container import --repo=%s --write-ref %s %s", tmprepo, ostreeref, sourceContainerRef)
			c.RunCmdSyncf(m, "ostree --repo=%s init --mode=archive", ostreeRepo)
			c.RunCmdSyncf(m, "ostree --repo=%s pull-local %s %s", ostreeRepo, tmprepo, ostreeref)
		}

	})

	c.Run("upgrade-from-previous", func(c cluster.TestCluster) {
		// We need to check now whether this is a within-stream update or a
		// cross-stream rebase.
		d, err := util.GetBootedDeployment(c, m)
		if err != nil {
			c.Fatal(err)
		}
		version := kola.CosaBuild.Meta.OstreeVersion
		if usingContainer {
			rpmostreeRebase(c, m, sourceContainerRef, version)
		} else if strings.HasSuffix(d.Origin, ":"+kola.CosaBuild.Meta.BuildRef) {
			// same stream; let's use Zincati
			graph.seedFromMachine(c, m)
			graph.addUpdate(c, m, version, kola.CosaBuild.Meta.OstreeCommit)
			waitForUpgradeToVersion(c, m, version)
		} else {
			rpmostreeRebase(c, m, kola.CosaBuild.Meta.BuildRef, version)
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
		ostreeCommit := kola.CosaBuild.Meta.OstreeCommit
		if usingContainer {
			newCommit := c.MustSSHf(m, "sudo ostree commit -b testupdate --tree=ref=%s --add-metadata-string version=%s", ostreeCommit, newVersion)
			rpmostreeRebase(c, m, string(newCommit), newVersion)
		} else {
			ostree_command := "ostree commit --repo %s -b %s --tree ref=%s --add-metadata-string version=%s " +
				"--keep-metadata='fedora-coreos.stream' --keep-metadata='coreos-assembler.basearch' --parent=%s"
			newCommit := c.MustSSHf(m,
				ostree_command,
				ostreeRepo, kola.CosaBuild.Meta.BuildRef, ostreeCommit, newVersion, ostreeCommit)

			graph.addUpdate(c, m, newVersion, string(newCommit))

			waitForUpgradeToVersion(c, m, newVersion)
		}
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

	if err := m.WaitForReboot(240*time.Second, oldBootId); err != nil {
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

func waitForUpgradeToBeStaged(c cluster.TestCluster, m platform.Machine) {
	// Here we set up a systemd path unit to watch for when ostree
	// behind the scenes updates the refs in the repo under the
	// /ostree/deploy directory.
	// Using /ostree/deploy as the canonical API for monitoring deployment changes.
	// This path is updated by ostree for deployment changes.
	// refchanged.path will trigger when it gets updated and will then stop wait.service.
	// The systemd-run --wait causes it to not return here (and thus
	// continue execution of code here) until wait.service has been
	// stopped by refchanged.service. This is an effort to make us
	// start waiting inside runFnAndWaitForRebootIntoVersion until
	// later in the upgrade process because we are seeing failures due
	// to timeouts and we're trying to reduce the variability by
	// minimizing the wait inside that function to just the actual reboot.
	// https://github.com/coreos/fedora-coreos-tracker/issues/1805
	//
	// Note: if systemd-run ever gains the ability to --wait when
	// generating a path unit then the below can be simplified.
	c.RunCmdSync(m, "sudo systemd-run -u refchanged --path-property=PathChanged=/ostree/deploy systemctl stop wait.service")
	c.RunCmdSync(m, "sudo systemd-run --wait -u wait sleep infinity")
}

func waitForUpgradeToVersion(c cluster.TestCluster, m platform.Machine, version string) {
	runFnAndWaitForRebootIntoVersion(c, m, version, func() {
		// Start Zincati so it will apply the update
		c.RunCmdSync(m, "sudo systemctl start zincati.service")
		waitForUpgradeToBeStaged(c, m)
	})
}

// rpmostreeRebase causes rpm-ostree to rebase and reboot into the targeted ref.  The provided
// version number will be verified post reboot.
func rpmostreeRebase(c cluster.TestCluster, m platform.Machine, ref, version string) {
	runFnAndWaitForRebootIntoVersion(c, m, version, func() {
		c.RunCmdSyncf(m, "sudo systemctl stop zincati")
		// we use systemd-run here so that we can test the --reboot path
		// without having SSH not exit cleanly, which would cause an error
		c.RunCmdSyncf(m, "sudo systemd-run rpm-ostree rebase --reboot %s", ref)
		waitForUpgradeToBeStaged(c, m)
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
