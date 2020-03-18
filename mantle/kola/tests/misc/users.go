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
	register.RegisterTest(&register.Test{
		Run:              CheckUserShells,
		ClusterSize:      1,
		ExcludePlatforms: []string{"gce"},
		Name:             "fcos.users.shells",
		Distros:          []string{"fcos"},
	})
}

func CheckUserShells(c cluster.TestCluster) {
	m := c.Machines()[0]
	var badusers []string

	ValidUsers := map[string]string{
		"sync":                 "/bin/sync",
		"shutdown":             "/sbin/shutdown",
		"halt":                 "/sbin/halt",
		"core":                 "/bin/bash",
		"fedora-coreos-pinger": "/usr/sbin/nologin",
		"zincati":              "/usr/sbin/nologin",
	}

	output := c.MustSSH(m, "getent passwd")

	users := strings.Split(string(output), "\n")

	for _, user := range users {
		userdata := strings.Split(user, ":")
		if len(userdata) != 7 {
			badusers = append(badusers, user)
			continue
		}

		username := userdata[0]
		shell := userdata[6]
		if username == "root" {
			// https://github.com/systemd/systemd/issues/15160
			if shell != "/bin/bash" && shell != "/bin/sh" {
				badusers = append(badusers, user)
			}
		} else if shell != ValidUsers[username] && shell != "/sbin/nologin" && shell != "/usr/sbin/nologin" {
			badusers = append(badusers, user)
		}
	}

	if len(badusers) != 0 {
		c.Fatalf("Invalid users: %v", badusers)
	}
}
