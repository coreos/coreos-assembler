// Copyright 2020 Red Hat
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
		Name:        "coreos.ignition.instantiated.enable-unit",
		Run:         enableSystemdInstantiatedService,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
    "ignition": {"version": "3.0.0"},
    "systemd": {
        "units": [{
			"name": "echo@.service",
			"contents": "[Unit]\nDescription=f\n[Service]\nType=oneshot\nExecStart=/bin/echo %i\nRemainAfterExit=yes\n[Install]\nWantedBy=multi-user.target\n"
		  },
		  {
		  "name": "echo@.timer",
		  "contents": "[Unit]\nDescription=echo timer template\n[Timer]\nOnUnitInactiveSec=10s\n[Install]\nWantedBy=timers.target"
		},
		{
		  "enabled": true,
		  "name": "echo@bar.service"
		},
		{
		  "enabled": true,
		  "name": "echo@foo.service"
		},
		{
			"enabled": true,
			"name": "echo@foo.timer"
		}]
    }
}`),
		// Enabling systemd instantiated services doesn't support
		// in a given system if the version of systemd is older than
		// 240. RHCOS deosn't support this feature currently because
		// its running an older version (v239) of systemd.
		ExcludeDistros: []string{"rhcos"},
	})
}

func enableSystemdInstantiatedService(c cluster.TestCluster) {
	m := c.Machines()[0]
	// MustSSH function will throw an error if the exit code
	// of the command is anything other than 0.
	c.RunCmdSync(m, "systemctl -q is-active echo@foo.service")
	c.RunCmdSync(m, "systemctl -q is-active echo@bar.service")
	c.RunCmdSync(m, "systemctl -q is-enabled echo@foo.service")
	c.RunCmdSync(m, "systemctl -q is-enabled echo@bar.service")
	c.RunCmdSync(m, "systemctl -q is-active echo@foo.timer")
	c.RunCmdSync(m, "systemctl -q is-enabled echo@foo.timer")
}
