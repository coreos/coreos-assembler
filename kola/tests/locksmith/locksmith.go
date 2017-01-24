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

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.locksmith.cluster",
		Run:         locksmithCluster,
		ClusterSize: 3,
		Platforms:   []string{"aws", "gce"},
		UserData: `{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd2.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd2 --name=$name --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379,http://0.0.0.0:4001 --listen-peer-urls=http://$private_ipv4:2380,http://$private_ipv4:7001"
        }]
      }
    ]
  },
  "files": [{
    "filesystem": "root",
    "path": "/etc/coreos/update.conf",
    "contents": { "source": "data:,REBOOT_STRATEGY=etcd-lock%0A" },
    "mode": 420
  }]
}`,
	})
}

func locksmithCluster(c cluster.TestCluster) error {
	machs := c.Machines()

	// Wait for all etcd cluster nodes to be ready.
	if err := etcd.GetClusterHealth(machs[0], len(machs)); err != nil {
		return fmt.Errorf("cluster health: %v", err)
	}

	output, err := machs[0].SSH("locksmithctl status")
	if err != nil {
		return fmt.Errorf("locksmithctl status: %q: %v", output, err)
	}

	ctx := context.Background()
	wg := worker.NewWorkerGroup(ctx, len(machs))

	// reboot all the things
	for _, m := range machs {
		worker := func(c context.Context) error {
			cmd := "sudo systemctl stop sshd.socket && sudo locksmithctl send-need-reboot"
			output, err := m.SSH(cmd)
			if _, ok := err.(*ssh.ExitMissingError); ok {
				err = nil // A terminated session is perfectly normal during reboot.
			}
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
