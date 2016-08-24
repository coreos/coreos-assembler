// Copyright 2016 CoreOS, Inc.
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

package locksmith

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.locksmith.cluster",
		Run:         locksmithCluster,
		ClusterSize: 3,
		UserData: `#cloud-config

coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001
`,
	})
}

const updateConf = `
GROUP=stable
REBOOT_STRATEGY=etcd-lock
`

func locksmithCluster(c cluster.TestCluster) error {
	machs := c.Machines()

	// copy locksmith config and restart locksmithd/etcd2.
	// XXX: can't block due to Type=notify triggering when the cluster has completed bootstrap.
	for _, m := range c.Machines() {
		if err := platform.InstallFile(strings.NewReader(updateConf), m, "/etc/coreos/update.conf"); err != nil {
			return fmt.Errorf("failed to write locksmith config to %s: %v", m.ID(), err)
		}

		output, err := m.SSH("sudo systemctl restart --no-block etcd2.service locksmithd.service")
		if err != nil {
			return fmt.Errorf("failed to start etcd2.service on %s: output %q: error: %v", m.ID(), output, err)
		}
	}

	// make sure etcd is ready
	etcdCheck := func() error {
		output, err := machs[0].SSH("locksmithctl status")
		if err != nil {
			return fmt.Errorf("cluster health: %q: %v", output, err)
		}
		return nil
	}

	if err := util.Retry(6, 5*time.Second, etcdCheck); err != nil {
		return fmt.Errorf("etcd bootstrap failed: %v", err)
	}

	ctx := context.Background()
	wg := worker.NewWorkerGroup(ctx, len(machs))

	// reboot all the things
	for _, m := range machs {
		worker := func(c context.Context) error {
			// XXX: stop sshd so checkmachine verifies correctly if reboot worked
			// XXX: run locksmithctl under systemd-run so our current connection doesn't drop suddenly
			cmd := "sudo systemctl stop sshd.socket; sudo systemd-run --quiet --on-active=2 --no-block locksmithctl send-need-reboot"
			output, err := m.SSH(cmd)
			if err != nil {
				return fmt.Errorf("failed to run %q: output: %q status: %q", cmd, output, err)
			}

			return platform.CheckMachine(m)
		}

		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}
	}

	return wg.Wait()
}
