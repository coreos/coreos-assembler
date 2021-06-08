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
	"fmt"
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.groups",
		Run:         groups,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
		             "ignition": { "version": "3.0.0" },
		             "systemd": {
		               "units": [{
		                 "name": "system-cloudinit@usr-share-coreos-developer_data.service",
		                 "mask": true
		               }]
		             },
		             "passwd": {
		               "groups": [
		                 {
		                   "name": "group1",
		                   "gid":  501
		                 },
		                 {
		                   "name": "group2",
		                   "gid":  502,
		                   "passwordHash": "foobar"
		                 }
		               ]
		             }
		           }`),
	})
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.v2.users",
		Run:         users,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
		             "ignition": { "version": "3.0.0" },
		             "passwd": {
		               "users": [
		                 {
		                   "name": "core",
		                   "passwordHash": "foobar"
		                 },
		                 {
		                   "name": "user1",
		                   "create": {}
		                 },
		                 {
		                   "name": "user2",
		                   "uid": 1010,
		                   "groups": [ "sudo" ]
		                 }
		               ]
		             }
		           }`),
	})
}

type userTest struct {
	user           string
	passwdRecord   string
	shadowPassword string
}

type groupTest struct {
	group         string
	groupRecord   string
	gshadowRecord string
}

func groups(c cluster.TestCluster) {
	m := c.Machines()[0]

	tests := []groupTest{
		{
			group:         "group1",
			groupRecord:   "group1:x:501:",
			gshadowRecord: "group1:*::",
		},
		{
			group:         "group2",
			groupRecord:   "group2:x:502:",
			gshadowRecord: "group2:foobar::",
		},
	}
	testGroup(c, m, tests)
}

func users(c cluster.TestCluster) {
	m := c.Machines()[0]

	tests := []userTest{
		{
			user:           "core",
			passwdRecord:   "core:x:1000:1000:CoreOS Admin:/var/home/core:/bin/bash",
			shadowPassword: "foobar",
		},
		{
			user:           "user1",
			passwdRecord:   "user1:x:1001:1001::/var/home/user1:/bin/bash",
			shadowPassword: "*",
		},
		{
			user:           "user2",
			passwdRecord:   "user2:x:1010:1010::/var/home/user2:/bin/bash",
			shadowPassword: "*",
		},
	}
	testUser(c, m, tests)
}

func testUser(c cluster.TestCluster, m platform.Machine, tests []userTest) {
	for _, t := range tests {
		if out, err := getent(c, m, "passwd", t.user); err != nil {
			c.Fatal(err)
		} else if out != t.passwdRecord {
			c.Errorf("%q wasn't correctly created: got %q, expected %q", t.user, out, t.passwdRecord)
		}

		out, err := getent(c, m, "shadow", t.user)
		if err != nil {
			c.Fatal(err)
		}

		fields := strings.Split(out, ":")
		if len(fields) < 2 {
			c.Fatalf("could not parse shadow record (%q) for %q", out, t.user)
		}

		if fields[0] != t.user || fields[1] != t.shadowPassword {
			c.Errorf("%q wasn't correctly created: got %q:%q, expected %q:%q", t.user, fields[0], fields[1], t.user, t.shadowPassword)
		}
	}
}

func testGroup(c cluster.TestCluster, m platform.Machine, tests []groupTest) {
	for _, t := range tests {
		if out, err := getent(c, m, "group", t.group); err != nil {
			c.Fatal(err)
		} else if out != t.groupRecord {
			c.Errorf("%q wasn't correctly created: got %q, expected %q", t.group, out, t.groupRecord)
		}
		if out, err := getent(c, m, "gshadow", t.group); err != nil {
			c.Fatal(err)
		} else if out != t.gshadowRecord {
			c.Errorf("%q wasn't correctly created: got %q, expected %q", t.group, out, t.gshadowRecord)
		}
	}
}

func getent(c cluster.TestCluster, m platform.Machine, database string, entry string) (string, error) {
	cmd := fmt.Sprintf("sudo getent %s %s", database, entry)
	if out, err := c.SSH(m, cmd); err == nil {
		return string(out), nil
	} else {
		return "", fmt.Errorf("failed to run `%s`: %s: %v", cmd, out, err)
	}
}
