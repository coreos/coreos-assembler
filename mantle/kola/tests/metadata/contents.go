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

package metadata

import (
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	enableMetadataService := conf.Ignition(`{
		"ignition": {"version": "3.0.0"},
		"systemd": {
			"units": [{
				"name": "afterburn.service",
				"enabled": true
			}, {
				"name": "metadata.target",
				"enabled": true,
				"contents": "[Install]\nWantedBy=multi-user.target"
			}]
		}
	}`)

	register.RegisterTest(&register.Test{
		Name:        "fcos.metadata.aws",
		Run:         verifyAWS,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		UserData:    enableMetadataService,
		Distros:     []string{"fcos"},
	})

	register.RegisterTest(&register.Test{
		Name:        "fcos.metadata.azure",
		Run:         verifyAzure,
		ClusterSize: 1,
		Platforms:   []string{"azure"},
		UserData:    enableMetadataService,
		Distros:     []string{"fcos"},
	})

	register.RegisterTest(&register.Test{
		Name:        "fcos.metadata.packet",
		Run:         verifyPacket,
		ClusterSize: 1,
		Platforms:   []string{"packet"},
		UserData:    enableMetadataService,
		Distros:     []string{"fcos"},
	})
}

func verifyAWS(c cluster.TestCluster) {
	verify(c, "AFTERBURN_AWS_IPV4_LOCAL", "AFTERBURN_AWS_IPV4_PUBLIC", "AFTERBURN_AWS_HOSTNAME")
}

func verifyAzure(c cluster.TestCluster) {
	verify(c, "AFTERBURN_AZURE_IPV4_DYNAMIC")
	// kola tests do not spawn machines behind a load balancer on Azure
	// which is required for AFTERBURN_AZURE_IPV4_VIRTUAL to be present
}

func verifyPacket(c cluster.TestCluster) {
	verify(c, "AFTERBURN_PACKET_HOSTNAME", "AFTERBURN_PACKET_PHONE_HOME_URL", "AFTERBURN_PACKET_IPV4_PUBLIC_0", "AFTERBURN_PACKET_IPV4_PRIVATE_0", "AFTERBURN_PACKET_IPV6_PUBLIC_0")
}

func verify(c cluster.TestCluster, keys ...string) {
	m := c.Machines()[0]

	out := c.MustSSH(m, "cat /run/metadata/afterburn")

	for _, key := range keys {
		if !strings.Contains(string(out), key) {
			c.Errorf("%q wasn't found in %q", key, string(out))
		}
	}
}
