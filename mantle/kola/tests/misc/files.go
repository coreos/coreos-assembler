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
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         Filesystem,
		ClusterSize: 1,
		Name:        "fcos.filesystem",
		Distros:     []string{"fcos"},
	})
}

func Filesystem(c cluster.TestCluster) {
	c.Run("suid", SUIDFiles)
	c.Run("sgid", SGIDFiles)
	c.Run("writablefiles", WritableFiles)
	c.Run("writabledirs", WritableDirs)
	c.Run("stickydirs", StickyDirs)
	c.Run("denylist", Denylist)
}

func sugidFiles(c cluster.TestCluster, validfiles []string, mode string) {
	m := c.Machines()[0]
	badfiles := make([]string, 0)

	output := c.MustSSH(m, fmt.Sprintf("sudo find / -ignore_readdir_race -path /sys -prune -o -path /proc -prune -o -path /sysroot/ostree -prune -o -type f -perm -%v -print", mode))

	if string(output) == "" {
		return
	}

	files := strings.Split(string(output), "\n")
	for _, file := range files {
		var valid bool

		for _, validfile := range validfiles {
			if file == validfile {
				valid = true
			}
		}
		if !valid {
			badfiles = append(badfiles, file)
		}
	}

	if len(badfiles) != 0 {
		c.Fatalf("Unknown SUID or SGID files found: %v", badfiles)
	}
}

func SUIDFiles(c cluster.TestCluster) {
	validfiles := []string{
		"/usr/bin/chage",
		"/usr/bin/chfn",
		"/usr/bin/chsh",
		"/usr/bin/expiry",
		"/usr/bin/fusermount",
		"/usr/bin/fusermount3",
		"/usr/bin/gpasswd",
		"/usr/bin/ksu",
		"/usr/bin/man",
		"/usr/bin/mandb",
		"/usr/bin/mount",
		"/usr/bin/newgidmap",
		"/usr/bin/newgrp",
		"/usr/bin/newuidmap",
		"/usr/bin/passwd",
		"/usr/bin/pkexec",
		"/usr/bin/umount",
		"/usr/bin/su",
		"/usr/bin/sudo",
		"/usr/lib/polkit-1/polkit-agent-helper-1",
		"/usr/lib64/polkit-1/polkit-agent-helper-1",
		"/usr/libexec/dbus-daemon-launch-helper",
		"/usr/libexec/sssd/krb5_child",
		"/usr/libexec/sssd/ldap_child",
		"/usr/libexec/sssd/selinux_child",
		"/usr/sbin/mount.nfs",
		"/usr/sbin/unix_chkpwd",
		"/usr/sbin/grub2-set-bootflag",
		"/usr/sbin/mount.nfs",
		"/usr/sbin/pam_timestamp_check",
	}

	sugidFiles(c, validfiles, "4000")
}

func SGIDFiles(c cluster.TestCluster) {
	validfiles := []string{
		"/usr/bin/write",
		"/usr/libexec/openssh/ssh-keysign",
		"/usr/libexec/utempter/utempter",
	}

	sugidFiles(c, validfiles, "2000")
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

func Denylist(c cluster.TestCluster) {
	m := c.Machines()[0]

	skip := []string{
		// Directories not to descend into
		"/proc",
		"/sys",
		"/var/lib/docker",
		"/sysroot/ostree",
		"/run/NetworkManager", // default connections include spaces
		"/run/udev",
		"/usr/lib/firmware",
	}

	denylist := []string{
		// Things excluded from the image that might slip in
		"/usr/bin/perl",
		"/usr/bin/python",
		"/usr/share/man",

		// net-tools "make install" copies binaries from
		// /usr/bin/{} to /usr/bin/{}.old before overwriting them.
		// This sometimes produced an extraneous set of {}.old
		// binaries due to make parallelism.
		// https://github.com/coreos/coreos-overlay/pull/2734
		"/usr/bin/*.old",

		// Control characters in filenames
		"*[\x01-\x1f]*",
		// Space
		"* *",
		// DEL
		"*\x7f*",
	}

	output := c.MustSSH(m, fmt.Sprintf("sudo find / -ignore_readdir_race -path %s -prune -o -path '%s' -print", strings.Join(skip, " -prune -o -path "), strings.Join(denylist, "' -print -o -path '")))

	if string(output) != "" {
		c.Fatalf("Denylisted files or directories found:\n%s", output)
	}
}
