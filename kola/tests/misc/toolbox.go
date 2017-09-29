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
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.Register(&register.Test{
		Run:              dnfInstall,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu"}, // Network access for toolbox
		Name:             "coreos.toolbox.dnf-install",
	})
}

// regression test for https://github.com/coreos/bugs/issues/1676
func dnfInstall(c cluster.TestCluster) {
	m := c.Machines()[0]

	output, err := c.SSH(m, `toolbox sh -c 'dnf install -y tcpdump; tcpdump --version >/dev/null && echo PASS' 2>/dev/null`)

	if err != nil {
		c.Fatalf("Error running dnf install in toolbox: %v", err)
	}

	if !strings.Contains(string(output), "PASS") {
		c.Fatalf("Expected 'pass' in output; got %v", string(output))
	}
}
