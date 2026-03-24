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
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         Filesystem,
		ClusterSize: 1,
		Name:        "fcos.filesystem",
		Description: "Verify the permissions are correct on the filesystem.",
		Distros:     []string{"fcos"},
	})
}

func Filesystem(c cluster.TestCluster) {
	c.Run("writablefiles", WritableFiles)
	c.Run("writabledirs", WritableDirs)
	c.Run("stickydirs", StickyDirs)
}

func WritableFiles(c cluster.TestCluster) {
	m := c.Machines()[0]

	output := c.MustSSH(m, "sudo find / -ignore_readdir_race -path /sys -prune -o -path /proc -prune -o -path /sysroot/ostree -prune -o -type f -perm -0002 -print")

	if string(output) != "" {
		c.Fatalf("Unknown writable files found: %s", output)
	}
}

func WritableDirs(c cluster.TestCluster) {
	m := c.Machines()[0]

	output := c.MustSSH(m, "sudo find / -ignore_readdir_race -path /sys -prune -o -path /proc -prune -o -path /sysroot/ostree -prune -o -type d -perm -0002 -a ! -perm -1000 -print")

	if string(output) != "" {
		c.Fatalf("Unknown writable directories found: %s", output)
	}
}

// The default permissions for the root of a tmpfs are 1777
// https://github.com/coreos/bugs/issues/1812
func StickyDirs(c cluster.TestCluster) {
	m := c.Machines()[0]

	ignore := []string{
		// don't descend into these
		"/proc",
		"/sys",
		"/var/lib/docker",
		"/sysroot/ostree",

		// should be sticky, and may have sticky children
		"/dev/mqueue",
		"/dev/shm",
		"/media",
		"/tmp",
		"/var/tmp",
		"/run/user/1000/libpod",
	}

	output := c.MustSSH(m, fmt.Sprintf("sudo find / -ignore_readdir_race -path %s -prune -o -type d -perm /1000 -print", strings.Join(ignore, " -prune -o -path ")))

	if string(output) != "" {
		c.Fatalf("Unknown sticky directories found: %s", output)
	}
}
