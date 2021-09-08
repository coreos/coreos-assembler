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
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.systemd.enable-service",
		Run:         enableSystemdService,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		// enable nfs-server, touch /etc/exports as it doesn't exist by default on Container Linux,
		// and touch /var/lib/nfs/etab (https://bugzilla.redhat.com/show_bug.cgi?id=1394395) for RHCOS
		UserData: conf.Ignition(`{
    "ignition": {"version": "3.0.0"},
    "systemd": {
        "units": [{
            "name":"nfs-server.service",
            "enabled":true
        }]
    },
    "storage": {
        "files": [{
            "path":"/etc/exports"
        },
        {
            "path":"/var/lib/nfs/etab"
        }]
    }
}`),
		// FCOS just ships the client (see
		// https://github.com/coreos/fedora-coreos-tracker/issues/121).
		// Should probably just pick a different unit to test with, though
		// testing the NFS workflow is useful for RHCOS.
		ExcludeDistros: []string{"fcos"},
	})
}

func enableSystemdService(c cluster.TestCluster) {
	m := c.Machines()[0]

	c.AssertCmdOutputContains(m, "systemctl show -p ActiveState nfs-server.service", "ActiveState=active")
}
