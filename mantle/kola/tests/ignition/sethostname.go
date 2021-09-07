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
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	// Set the hostname
	config := conf.Ignition(`{
		          "ignition": {
		              "version": "3.0.0"
		          },
		          "storage": {
		              "files": [
		                  {
		                      "path": "/etc/hostname",
		                      "mode": 420,
							  "overwrite": true,
		                      "contents": {
		                          "source": "data:,core1"
		                      }
		                  }
		              ]
		          }
		      }`)

	// These tests are disabled on Azure because the hostname
	// is required by the API and is overwritten via waagent.service
	// after the machine has booted.
	register.RegisterTest(&register.Test{
		Name:             "coreos.ignition.sethostname",
		Run:              setHostname,
		ClusterSize:      1,
		UserData:         config,
		ExcludePlatforms: []string{"azure"},
		Tags:             []string{"ignition"},
	})
}

func setHostname(c cluster.TestCluster) {
	m := c.Machines()[0]
	c.AssertCmdOutputContains(m, "hostnamectl", "Static hostname: core1")
}
