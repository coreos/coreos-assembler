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

package fleet

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/fleet")

	masterconf = `{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd2.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd2 --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379,http://0.0.0.0:4001 --listen-peer-urls=http://$private_ipv4:2380,http://$private_ipv4:7001"
        }]
      },
      {
        "name": "fleet.service",
        "enable": true,
        "dropins": [{
          "name": "environment.conf",
          "contents": "[Service]\nEnvironment=FLEET_ETCD_REQUEST_TIMEOUT=15"
        }]
      }
    ]
  },
  "storage": {
    "files": [{
      "filesystem": "root",
      "path": "/etc/hostname",
      "contents": { "source": "data:,master" },
      "mode": 420
    }]
  }
}`

	proxyconf = `{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd2.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd2 --discovery=$discovery --proxy=on --listen-client-urls=http://0.0.0.0:2379,http://0.0.0.0:4001"
        }]
      },
      {
        "name": "fleet.service",
        "enable": true
      }
    ]
  },
  "storage": {
    "files": [
      {
        "filesystem": "root",
        "path": "/etc/hostname",
        "contents": { "source": "data:,proxy" },
        "mode": 420
      },
      {
        "filesystem": "root",
        "path": "/home/core/hello.service",
        "contents": { "source": "data:,%5BUnit%5D%0ADescription=simple%20fleet%20test%0A%5BService%5D%0AExecStart=/bin/sh%20-c%20%22while%20sleep%201%3B%20do%20echo%20hello%20world%3B%20done%22" },
        "mode": 420
      }
    ]
  }
}`
)

func init() {
	register.Register(&register.Test{
		Run:         Proxy,
		ClusterSize: 0,
		Name:        "coreos.fleet.etcdproxy",
		Platforms:   []string{"aws", "gce"},
		UserData:    `#cloud-config`,
	})
}

// Test fleet running through an etcd2 proxy.
func Proxy(c cluster.TestCluster) error {
	discoveryURL, _ := c.GetDiscoveryURL(1)

	master, err := c.NewMachine(strings.Replace(masterconf, "$discovery", discoveryURL, -1))
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine master: %s", err)
	}
	defer master.Destroy()

	proxy, err := c.NewMachine(strings.Replace(proxyconf, "$discovery", discoveryURL, -1))
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine proxy: %s", err)
	}
	defer proxy.Destroy()

	// Wait for all etcd cluster nodes to be ready.
	if err = etcd.GetClusterHealth(master, 1); err != nil {
		return fmt.Errorf("cluster health master: %v", err)
	}
	if err = etcd.GetClusterHealth(proxy, 1); err != nil {
		return fmt.Errorf("cluster health proxy: %v", err)
	}

	// Several seconds can pass after etcd is ready before fleet notices.
	fleetStart := func() error {
		_, err = proxy.SSH("fleetctl start /home/core/hello.service")
		if err != nil {
			return fmt.Errorf("fleetctl start: %v", err)
		}
		return nil
	}
	if err := util.Retry(5, 5*time.Second, fleetStart); err != nil {
		return fmt.Errorf("fleetctl start failed: %v", err)
	}

	status, err := proxy.SSH("fleetctl list-units -l -fields active -no-legend")
	if err != nil {
		return fmt.Errorf("fleetctl list-units failed: %v", err)
	}

	if !bytes.Equal(status, []byte("active")) {
		return fmt.Errorf("unit not active")
	}

	return nil
}
