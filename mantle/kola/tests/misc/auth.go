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

package misc

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         AuthVerify,
		ClusterSize: 1,
		Name:        "coreos.auth.verify",
	})
}

// Basic authentication tests.

// AuthVerify asserts that invalid passwords do not grant access to the system
func AuthVerify(c cluster.TestCluster) {
	m := c.Machines()[0]

	client, err := m.PasswordSSHClient("core", "asdf")
	if err == nil {
		client.Close()
		c.Fatalf("Successfully authenticated despite invalid password auth")
	}
}
