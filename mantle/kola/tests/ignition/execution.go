// Copyright 2017 CoreOS, Inc.
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
		Name:        "coreos.ignition.once",
		Run:         runsOnce,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
                             "ignition": { "version": "3.0.0" },
                             "storage": {
                               "files": [
                                 {
                                   "path": "/etc/ignition-ran",
                                   "contents": {
                                     "source": "data:,Ignition%20ran."
                                   },
                                   "mode": 420
                                 }
                               ]
                             }
                           }`),
	})
}

func runsOnce(c cluster.TestCluster) {
	m := c.Machines()[0]

	// remove file created by Ignition; fail if it doesn't exist
	c.RunCmdSync(m, "sudo rm /etc/ignition-ran")

	err := m.Reboot()
	if err != nil {
		c.Fatalf("Couldn't reboot machine: %v", err)
	}

	// make sure file hasn't been recreated
	c.RunCmdSync(m, "test ! -e /etc/ignition-ran")
}
