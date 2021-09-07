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

package ignition

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.symlink",
		Run:         writeAbsoluteSymlink,
		ClusterSize: 1,
		Platforms:   []string{"qemu"},
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "3.0.0"
		  },
		  "storage": {
		      "links": [
		          {
		              "group": {
		                  "name": "core"
		              },
		              "overwrite": true,
		              "path": "/etc/localtime",
		              "user": {
		                  "name": "core"
		              },
		              "hard": false,
		              "target": "/usr/share/zoneinfo/Europe/Zurich"
		          }
		      ]
		  }
	      }`),
	})
}

func writeAbsoluteSymlink(c cluster.TestCluster) {
	m := c.Machines()[0]

	c.AssertCmdOutputContains(m, "readlink /etc/localtime", "/usr/share/zoneinfo/Europe/Zurich")
}
