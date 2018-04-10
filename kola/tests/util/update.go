// Copyright 2018 CoreOS, Inc.
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

package util

import (
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
)

func AssertBootedUsr(c cluster.TestCluster, m platform.Machine, usr string) {
	usrdev := GetUsrDeviceNode(c, m)
	target := c.MustSSH(m, "readlink -f /dev/disk/by-partlabel/"+usr)
	if usrdev != string(target) {
		c.Fatalf("Expected /usr to be %v (%s) but it is %v", usr, target, usrdev)
	}
}

func GetUsrDeviceNode(c cluster.TestCluster, m platform.Machine) string {
	// find /usr dev
	usrdev := c.MustSSH(m, "findmnt -no SOURCE /usr")

	// XXX: if the /usr dev is /dev/mapper/usr, we're on a verity enabled
	// image, so use dmsetup to find the real device.
	if strings.TrimSpace(string(usrdev)) == "/dev/mapper/usr" {
		usrdev = c.MustSSH(m, "echo -n /dev/$(sudo dmsetup info --noheadings -Co blkdevs_used usr)")
	}

	return string(usrdev)
}
