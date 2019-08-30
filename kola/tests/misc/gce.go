// Copyright 2019 Red Hat, Inc.
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

package misc

import (
	"encoding/json"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.misc.gce.agent",
		Platforms:   []string{"gce"},
		Run:         gceVerifyAgentIsRunning,
		ClusterSize: 1,
		Distros:     []string{"cl"},
	})
}

type rktEntry struct {
	State     string
	App_Names []string
}

func gceVerifyAgentIsRunning(c cluster.TestCluster) {
	list := c.MustSSH(c.Machines()[0], "rkt list --format json")
	rktlist := []rktEntry{}
	json.Unmarshal(list, &rktlist)
	if len(rktlist) != 1 {
		// either it didn't start or it failed and now there's two
		c.Fatalf("gce agent is not running")
	}
	ent := rktlist[0]
	if ent.State != "running" {
		c.Fatalf("gce agent is not running, instead is: %v", rktlist[0].State)
	}
	if len(ent.App_Names) != 1 {
		c.Fatalf("missing app name")
	}
	if ent.App_Names[0] != "oem-gce" {
		c.Fatalf("running rkt container was not gce agent")
	}
}
