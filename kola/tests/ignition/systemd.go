// Copyright 2018 Red Hat
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
	register.Register(&register.Test{
		Name:        "coreos.ignition.systemd.enable-service",
		Run:         enableSystemdService,
		ClusterSize: 1,
		// enable nfs-server & touch /etc/exports as it doesn't exist by default on Container Linux
		UserData: conf.Ignition(`{
    "ignition": {"version": "2.2.0"},
    "systemd": {
        "units": [{
            "name":"nfs-server.service",
            "enabled":true
        }]
    },
    "storage": {
        "files": [{
            "filesystem":"root",
            "path":"/etc/exports"
        }]
    }
}`),
	})
}

func enableSystemdService(c cluster.TestCluster) {
	m := c.Machines()[0]

	out := c.MustSSH(m, "systemctl status nfs-server.service")
	if strings.Contains(string(out), "inactive") {
		c.Fatalf("service was not enabled or systemd-presets did not run")
	}
}
