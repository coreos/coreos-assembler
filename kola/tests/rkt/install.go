// Copyright 2015 CoreOS, Inc.
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

package rkt

import (
	"fmt"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         Install,
		ClusterSize: 0,
		Name:        "coreos.rkt.install",
	})
}

// Test to make sure rkt install works.
func Install(c platform.TestCluster) error {
	mach, err := c.NewMachine("")
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine: %s", err)
	}
	defer mach.Destroy()

	cmd := "sudo rkt install"
	output, err := mach.SSH(cmd)
	if err != nil {
		return fmt.Errorf("failed to run %q: %s: %s", cmd, err, output)
	}

	return nil
}
