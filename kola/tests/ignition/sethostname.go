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

package ignition

import (
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	// Set the hostname
	configV1 := conf.Ignition(`{
		          "ignitionVersion": 1,
		          "storage": {
		              "filesystems": [
		                  {
		                      "device": "/dev/disk/by-partlabel/ROOT",
		                      "format": "ext4",
		                      "files": [
		                          {
		                              "path": "/etc/hostname",
		                              "mode": 420,
		                              "contents": "core1"
		                          }
		                      ]
		                  }
		              ]
		          }
		      }`)
	configV2 := conf.Ignition(`{
		          "ignition": {
		              "version": "2.0.0"
		          },
		          "storage": {
		              "files": [
		                  {
		                      "filesystem": "root",
		                      "path": "/etc/hostname",
		                      "mode": 420,
		                      "contents": {
		                          "source": "data:,core1"
		                      }
		                  }
		              ]
		          }
		      }`)
	register.Register(&register.Test{
		Name:        "coreos.ignition.v1.sethostname",
		Run:         setHostname,
		ClusterSize: 1,
		UserData:    configV1,
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.sethostname",
		Run:         setHostname,
		ClusterSize: 1,
		UserData:    configV2,
	})
}

func setHostname(c cluster.TestCluster) {
	m := c.Machines()[0]

	out, err := c.SSH(m, "hostnamectl")
	if err != nil {
		c.Fatalf("failed to run hostnamectl: %s: %v", out, err)
	}

	if !strings.Contains(string(out), "Static hostname: core1") {
		c.Fatalf("hostname wasn't set correctly:\n%s", out)
	}
}
