// Copyright 2015 CoreOS, Inc.
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

package etcd

import (
	"fmt"
	"strings"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/etcd")

func init() {
	// test etcd discovery with 0.4.7
	register.Register(&register.Test{
		Run:         Discovery,
		Manual:      true,
		ClusterSize: 3,
		Name:        "coreos.etcd0.discovery",
		Platforms:   []string{"aws", "gce"},
		UserData: `{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd --name=$name --discovery=$discovery --addr=$private_ipv4:2379 --peer-addr=$private_ipv4:2380"
        }]
      }
    ]
  }
}`,
	})

	// test etcd discovery with 2.0 with new cloud config
	register.Register(&register.Test{
		Run:         Discovery,
		ClusterSize: 3,
		Name:        "coreos.etcd2.discovery",
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
  }
}`,
	})
}

func Discovery(c cluster.TestCluster) error {
	var err error

	if plog.LevelAt(capnslog.DEBUG) {
		// get journalctl -f from all machines before starting
		for _, m := range c.Machines() {
			if err = platform.StreamJournal(m); err != nil {
				return fmt.Errorf("failed to start journal: %v", err)
			}
		}
	}

	// NOTE(pb): this check makes the next code somewhat redundant
	if err = GetClusterHealth(c.Machines()[0], len(c.Machines())); err != nil {
		return fmt.Errorf("discovery failed cluster-health check: %v", err)
	}

	var keyMap map[string]string
	keyMap, err = setKeys(c, 5)
	if err != nil {
		return fmt.Errorf("failed to set keys: %v", err)
	}

	var quorumRead bool
	quorumRead = strings.Contains(c.Name, "etcd2")
	if err = checkKeys(c, keyMap, quorumRead); err != nil {
		return fmt.Errorf("failed to check keys: %v", err)
	}

	return nil
}
