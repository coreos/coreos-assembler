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
	"fmt"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.Register(&register.Test{
		Run:         SelinuxEnforce,
		ClusterSize: 1,
		Name:        "coreos.selinux.enforce",
		UserData:    `#cloud-config`,
	})
}

// SelinuxEnforce checks that some basic things work after `setenforce 1`
func SelinuxEnforce(c cluster.TestCluster) error {
	m := c.Machines()[0]

	for _, cmd := range []struct {
		cmdline     string
		checkoutput bool
		output      string
	}{
		{"sudo setenforce 1", false, ""},
		{"getenforce", true, "Enforcing"},
		{"systemctl --no-pager is-active system.slice", true, "active"},
	} {
		output, err := m.SSH(cmd.cmdline)
		if err != nil {
			return fmt.Errorf("failed to run %q: output: %q status: %q", cmd.cmdline, output, err)
		}

		if cmd.checkoutput && string(output) != cmd.output {
			return fmt.Errorf("command %q has unexpected output: want %q got %q", cmd.cmdline, cmd.output, string(output))
		}
	}

	return nil
}
