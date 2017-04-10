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
		Run:         CheckUserShells,
		ClusterSize: 1,
		Platforms:   []string{"qemu", "aws"},
		Name:        "coreos.users.shells",
		UserData:    `#cloud-config`,
	})
}

func CheckUserShells(c cluster.TestCluster) {
	m := c.Machines()[0]
	var badusers []string

	ValidUsers := map[string]string{
		"root":     "/bin/bash",
		"sync":     "/bin/sync",
		"shutdown": "/sbin/shutdown",
		"halt":     "/sbin/halt",
		"core":     "/bin/bash",
	}

	output, err := m.SSH("getent passwd")
	if err != nil {
		c.Fatalf("Failed to run grep: output %s, status: %v", output, err)
	}

	users := strings.Split(string(output), "\n")

	for _, user := range users {
		userdata := strings.Split(user, ":")
		if len(userdata) != 7 {
			badusers = append(badusers, user)
			continue
		}

		username := userdata[0]
		shell := userdata[6]
		if shell != ValidUsers[username] && shell != "/sbin/nologin" {
			badusers = append(badusers, user)
		}
	}

	if len(badusers) != 0 {
		c.Fatalf("Invalid users: %v", badusers)
	}
}
