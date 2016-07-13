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

package systemd

import (
	"fmt"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         gshadowParser,
		ClusterSize: 1,
		Name:        "systemd.sysusers.gshadow",
		UserData:    `#cloud-config`,
		MinVersion:  semver.Version{Major: 1095},
	})
}

// Verify that glibc's parsing of /etc/gshadow does not cause systemd-sysusers
// to segfault on specially constructed lines.
//
// One line must fit into the character buffer (1024 bytes, unless a previous
// line was longer) but have enough group members such that
//
//     line length + alignment + sizeof(char *) * (#adm + 1 + #mem + 1) > 1024.
//
// The parser would return early to avoid overflow, leaving the static result
// struct pointing to pointers from the previous line which are now invalid,
// causing segfaults when those pointers are dereferenced.
//
// Tests: https://github.com/coreos/bugs/issues/1394
func gshadowParser(c platform.TestCluster) error {
	m := c.Machines()[0]

	for _, cmd := range []string{
		`sudo sh -c "echo 'grp0:*::root' >> /etc/gshadow"`,
		`sudo sh -c "echo 'grp1:*::somebody.a1,somebody.a2,somebody.a3,somebody.a4,somebody.a5,somebody.a6,somebody.a7,somebody.a8,somebody.a9,somebody.a10,somebody.a11,somebody.a12,somebody.a13,somebody.a14,somebody.a15,somebody.a16,somebody.a17,somebody.a18,somebody.a19,somebody.a20,somebody.a21,somebody.a22,somebody.a23,somebody.a24,somebody.a25,somebody.a26,somebody.a27,somebody.a28,somebody.a29,somebody.a30,somebody.a31,somebody.a32,somebody.a33,somebody.a34,somebody.a35,somebody.a36,somebody.a37,somebody.a38,somebody.a39,somebody.a40,somebody.a41,somebody.a42,somebody.a43,somebody.a44,somebody.a45,somebody.a46,somebody.a47,a1234' >> /etc/gshadow"`,
		`sudo sh -c "echo 'grp2:*::root' >> /etc/gshadow"`,
		`sudo systemd-sysusers`,
	} {
		output, err := m.SSH(cmd)
		if err != nil {
			return fmt.Errorf("failed to run %q: output: %q status: %v", cmd, output, err)
		}
	}

	return nil
}
