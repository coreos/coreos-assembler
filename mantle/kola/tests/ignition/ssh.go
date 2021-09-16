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
	// verify that SSH key injection works correctly through Ignition,
	// without injecting via platform metadata
	register.RegisterTest(&register.Test{
		Name:             "coreos.ignition.ssh.key",
		Run:              noAfterburnSSHKey,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu"}, // redundant on qemu
		Flags:            []register.Flag{register.NoSSHKeyInMetadata},
		UserData:         conf.Ignition(`{"ignition":{"version":"3.0.0"}}`),
		Tags:             []string{"ignition"},
	})
}

func noAfterburnSSHKey(c cluster.TestCluster) {
	m := c.Machines()[0]
	// check that the test harness correctly skipped passing SSH keys
	// via Afterburn
	c.RunCmdSync(m, "[ ! -e ~/.ssh/authorized_keys.d/afterburn ]")
}
