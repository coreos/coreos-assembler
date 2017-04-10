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
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.once",
		Run:         runsOnce,
		ClusterSize: 1,
		UserData: `{
                             "ignition": { "version": "2.0.0" },
                             "storage": {
                               "files": [
                                 {
                                   "filesystem": "root",
                                   "path": "/etc/ignition-ran",
                                   "contents": {
                                     "source": "data:,Ignition%20ran."
                                   }
                                 }
                               ]
                             }
                           }`,
	})
}

func runsOnce(c cluster.TestCluster) {
	m := c.Machines()[0]

	// remove file created by Ignition; fail if it doesn't exist
	_, err := m.SSH("sudo rm /etc/ignition-ran")
	if err != nil {
		c.Fatalf("Couldn't remove flag file: %v", err)
	}

	err = m.Reboot()
	if err != nil {
		c.Fatalf("Couldn't reboot machine: %v", err)
	}

	// make sure file hasn't been recreated
	_, err = m.SSH("test -e /etc/ignition-ran")
	if err == nil {
		c.Fatalf("Flag file recreated after reboot")
	}
}
