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
	// Tests for https://github.com/coreos/bugs/issues/1184
	register.Register(&register.Test{
		Name:             "cl.ignition.misc.empty",
		Run:              empty,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu", "esx"},
		Distros:          []string{"cl"},
		UserData:         conf.Empty(),
	})
	// Tests for https://github.com/coreos/bugs/issues/1981
	register.Register(&register.Test{
		Name:             "cl.ignition.v1.noop",
		Run:              empty,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu", "esx", "openstack"},
		Distros:          []string{"cl"},
		Flags:            []register.Flag{register.NoSSHKeyInUserData},
		UserData:         conf.Ignition(`{"ignitionVersion": 1}`),
	})
	register.Register(&register.Test{
		Name:             "cl.ignition.v2.noop",
		Run:              empty,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu", "esx", "openstack"},
		Distros:          []string{"cl"},
		Flags:            []register.Flag{register.NoSSHKeyInUserData},
		UserData:         conf.Ignition(`{"ignition":{"version":"2.0.0"}}`),
	})
}

func empty(_ cluster.TestCluster) {
}
