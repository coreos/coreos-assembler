// Copyright 2021 Red Hat, Inc.
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

package misc

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

var (
	dhclientconf = conf.ContainerLinuxConfig(`storage:
  files:
    - filesystem: "root"
      path: "etc/NetworkManager/conf.d/dhcp-client.conf"
      contents:
		inline: |
		  [main]
		  dhcp=dhclient
      mode: 0644`)
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         dhcp,
		ClusterSize: 0,
		Name:        "coreos.dhcp.verify",
		Distros:     []string{"rhcos"},
	})
}

func dhcp(c cluster.TestCluster) {
	m1, err := c.NewMachine(nil)
	if err != nil {
		c.Fatalf("Cluster.NewMachine: %s", err)
	}
	defer m1.Destroy()

	c.Log("System with internal NetworkManager DHCP client booted.")

	nmlogs := c.MustSSH(m1, "journalctl -u NetworkManager --grep=\"dhcp-init: Using DHCP client 'internal'\" -b 0")
	c.Logf("internal NM DHCP logs on server: %s", nmlogs)

	m2, err := c.NewMachine(dhclientconf)
	if err != nil {
		c.Fatalf("Cluster.NewMachine: %s", err)
	}
	defer m2.Destroy()

	c.Log("System with dhclient booted.")

	dhclientlogs := c.MustSSH(m1, "journalctl -b 0 -u NetworkManager --grep=dhclient")
	c.Logf("dhclient journal logs on server: %s", dhclientlogs)
}
