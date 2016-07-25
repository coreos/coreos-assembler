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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         SUIDFiles,
		ClusterSize: 1,
		Name:        "coreos.filesystem.suid",
		UserData:    `#cloud-config`,
	})
	register.Register(&register.Test{
		Run:         SGIDFiles,
		ClusterSize: 1,
		Name:        "coreos.filesystem.sgid",
		UserData:    `#cloud-config`,
	})
	register.Register(&register.Test{
		Run:         WritableFiles,
		ClusterSize: 1,
		Name:        "coreos.filesystem.writablefiles",
		UserData:    `#cloud-config`,
	})
	register.Register(&register.Test{
		Run:         WritableDirs,
		ClusterSize: 1,
		Name:        "coreos.filesystem.writabledirs",
		UserData:    `#cloud-config`,
	})
}

func sugidFiles(m platform.Machine, validfiles []string, mode string) error {
	badfiles := make([]string, 0, 0)

	command := fmt.Sprintf("sudo find / -path /sys -prune -o -path /proc -prune -o -type f -perm -%v -print", mode)

	output, err := m.SSH(command)
	if err != nil {
		return fmt.Errorf("Failed to run find: output %v, status: %v", output, err)
	}

	if string(output) == "" {
		return nil
	}

	files := strings.Split(string(output), "\n")
	for _, file := range files {
		var valid bool

		for _, validfile := range validfiles {
			if file == validfile {
				valid = true
			}
		}
		if valid != true {
			badfiles = append(badfiles, file)
		}
	}

	if len(badfiles) != 0 {
		return fmt.Errorf("Unknown SUID or SGID files found: %v", badfiles)
	}

	return nil
}

func SUIDFiles(c cluster.TestCluster) error {
	m := c.Machines()[0]

	validfiles := []string{
		"/usr/bin/chage",
		"/usr/bin/chfn",
		"/usr/bin/chsh",
		"/usr/bin/expiry",
		"/usr/bin/gpasswd",
		"/usr/bin/ksu",
		"/usr/bin/man",
		"/usr/bin/mandb",
		"/usr/bin/mount",
		"/usr/bin/newgrp",
		"/usr/bin/passwd",
		"/usr/bin/pkexec",
		"/usr/bin/umount",
		"/usr/bin/su",
		"/usr/bin/sudo",
		"/usr/lib64/polkit-1/polkit-agent-helper-1",
		"/usr/libexec/dbus-daemon-launch-helper",
		"/usr/sbin/mount.nfs",
		"/usr/sbin/unix_chkpwd",
	}

	return sugidFiles(m, validfiles, "4000")
}

func SGIDFiles(c cluster.TestCluster) error {
	m := c.Machines()[0]

	validfiles := []string{}

	return sugidFiles(m, validfiles, "2000")
}

func WritableFiles(c cluster.TestCluster) error {
	m := c.Machines()[0]

	output, err := m.SSH("sudo find / -regextype posix-extended -regex \"/(sys|proc)\" -prune -o -type f -perm -0002 -print")
	if err != nil {
		return fmt.Errorf("Failed to run find: output %v, status: %v", output, err)
	}

	if string(output) != "" {
		return fmt.Errorf("Unknown writable files found: %v", output)
	}

	return nil
}

func WritableDirs(c cluster.TestCluster) error {
	m := c.Machines()[0]

	output, err := m.SSH("sudo find / -regextype posix-extended -regex \"/(sys|proc)\" -prune -o -type d -perm -0002 -a ! -perm -1000 -print")
	if err != nil {
		return fmt.Errorf("Failed to run find: output %v, status: %v", output, err)
	}

	if string(output) != "" {
		return fmt.Errorf("Unknown writable directories found: %v", output)
	}

	return nil
}
