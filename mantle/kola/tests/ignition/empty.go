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

// These tests require the kola key to be passed to the instance via cloud
// provider metadata since it will not be injected into the config. Platforms
// where the cloud provider metadata system is not available have been excluded.
func init() {
	register.RegisterTest(&register.Test{
		Name:             "fcos.ignition.misc.empty",
		Run:              noIgnitionSSHKey,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu", "esx"},
		Distros:          []string{"fcos"},
		UserData:         conf.Empty(),
		Tags:             []string{"ignition"},
	})
	register.RegisterTest(&register.Test{
		Name:             "fcos.ignition.v3.noop",
		Run:              noIgnitionSSHKey,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu", "esx"},
		Distros:          []string{"fcos"},
		Flags:            []register.Flag{register.NoSSHKeyInUserData},
		UserData:         conf.Ignition(`{"ignition":{"version":"3.0.0"}}`),
		Tags:             []string{"ignition"},
	})
}

func noIgnitionSSHKey(c cluster.TestCluster) {
	m := c.Machines()[0]
	// check that the test harness correctly skipped passing SSH keys
	// via Ignition
	c.RunCmdSync(m, "[ ! -e ~/.ssh/authorized_keys.d/ignition ]")
}
