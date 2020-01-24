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

package packages

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

// init runs when the package is imported and takes care of registering tests
func init() {
	register.RegisterTest(&register.Test{
		Run:         noPythonTest,
		ClusterSize: 1,
		Name:        `fcos.python`,
		Distros:     []string{"fcos"},
	})
}

// Test: Verify python is not installed
func noPythonTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	out, err := c.SSH(m, `rpm -q python2`)
	if err == nil {
		c.Fatalf("%s should not be installed", out)
	}

	out, err = c.SSH(m, `rpm -q python3`)
	if err == nil {
		c.Fatalf("%s should not be installed", out)
	}
}
