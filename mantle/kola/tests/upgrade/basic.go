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
	"fmt"
	"net/http"
	"path/filepath"
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

	containerImageFilename := kola.CosaBuild.Meta.BuildArtifacts.Ostree.Path

	sourceContainerRef := fmt.Sprintf("ostree-unverified-image:oci-archive:%s", containerImageFilename)

	c.Run("setup", func(c cluster.TestCluster) {
		// this is the only heavy-weight part, though remember this test is
		// optimized for qemu testing locally where this won't leave localhost at
		// all. cloud testing should mostly be a pipeline thing, where the infra
		// connection should be much faster
		ociArchivePath := filepath.Join(kola.CosaBuild.Dir, containerImageFilename)
		if err := cluster.DropFile(c.Machines(), ociArchivePath); err != nil {
			c.Fatal(err)
		}

		// In the container path we'll pass this file directly, so put it outside
		// of the user's home directory so the systemd service can find it.
		c.RunCmdSyncf(m, "sudo mv %s /var/tmp/%s", containerImageFilename, containerImageFilename)
		sourceContainerRef = fmt.Sprintf("ostree-unverified-image:oci-archive:/var/tmp/%s", containerImageFilename)

	})

	c.Run("upgrade-from-previous", func(c cluster.TestCluster) {
		version := kola.CosaBuild.Meta.OstreeVersion
		rpmostreeRebase(c, m, sourceContainerRef, version)
	})

	// Now, synthesize an update and serve that -- this is similar to
	// `rpmostree.upgrade-rollback`, but the major difference here is that the
	// starting disk is the previous release (and also, we're doing this via
	// Zincati & HTTP). Essentially, this sanity-checks that old starting state
	// + new content set can update.

	c.Run("upgrade-from-current", func(c cluster.TestCluster) {
		newVersion := kola.CosaBuild.Meta.OstreeVersion + ".kola"
		// until https://github.com/bootc-dev/bootc/pull/1421 propagates, we can't rely on kola.CosaBuild.Meta.OstreeCommit being the same
		ostreeCommit := c.MustSSHf(m, "sudo rpm-ostree status --json | jq -r '.deployments[0].checksum'")
		newCommit := c.MustSSHf(m, "sudo ostree commit -b testupdate --tree=ref=%s --add-metadata-string version=%s", ostreeCommit, newVersion)
		rpmostreeRebase(c, m, string(newCommit), newVersion)
	})
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
