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

package ignition

import (
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	enableMetadataService := conf.Ignition(`{
	    "ignitionVersion": 1,
	    "systemd": {
		"units": [
		    {
			"name": "coreos-metadata.service",
			"enable": true
		    },
		    {
			"name": "metadata.target",
			"enable": true,
			"contents": "[Install]\nWantedBy=multi-user.target"
		    }
		]
	    }
	}`)

	register.Register(&register.Test{
		Name:        "coreos.metadata.aws",
		Run:         verifyAWS,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		UserData:    enableMetadataService,
	})

	register.Register(&register.Test{
		Name:        "coreos.metadata.azure",
		Run:         verifyAzure,
		ClusterSize: 1,
		Platforms:   []string{"azure"},
		UserData:    enableMetadataService,
	})

	register.Register(&register.Test{
		Name:        "coreos.metadata.packet",
		Run:         verifyPacket,
		ClusterSize: 1,
		Platforms:   []string{"packet"},
		UserData:    enableMetadataService,
	})
}

func verifyAWS(c cluster.TestCluster) {
	verify(c, "COREOS_EC2_IPV4_LOCAL", "COREOS_EC2_IPV4_PUBLIC", "COREOS_EC2_HOSTNAME")
}

func verifyAzure(c cluster.TestCluster) {
	verify(c, "COREOS_AZURE_IPV4_DYNAMIC", "COREOS_AZURE_IPV4_VIRTUAL")
}

func verifyPacket(c cluster.TestCluster) {
	verify(c, "COREOS_PACKET_HOSTNAME", "COREOS_PACKET_PHONE_HOME_URL", "COREOS_PACKET_IPV4_PUBLIC_0", "COREOS_PACKET_IPV4_PRIVATE_0", "COREOS_PACKET_IPV6_PUBLIC_0")
}

func verify(c cluster.TestCluster, keys ...string) {
	m := c.Machines()[0]

	out, err := c.SSH(m, "cat /run/metadata/coreos")
	if err != nil {
		c.Fatalf("failed to cat /run/metadata/coreos: %s: %v", out, err)
	}

	for _, key := range keys {
		if !strings.Contains(string(out), key) {
			c.Errorf("%q wasn't found in %q", key, string(out))
		}
	}
}
