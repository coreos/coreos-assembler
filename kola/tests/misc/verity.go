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
	"bytes"
	"fmt"
	"strings"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/machine/qemu"
)

func init() {
	register.Register(&register.Test{
		Run:         Verity,
		ClusterSize: 1,
		Name:        "coreos.verity",
		Distros:     []string{"cl"},
	})
}

func Verity(c cluster.TestCluster) {
	c.Run("verify", VerityVerify)
	// modifies disk; must run last
	c.Run("corruption", VerityCorruption)
}

// Verity verification tests.
// TODO(mischief): seems like a good candidate for kolet.

// VerityVerify asserts that the filesystem mounted on /usr matches the
// dm-verity hash that is embedded in the CoreOS kernel.
func VerityVerify(c cluster.TestCluster) {
	m := c.Machines()[0]

	// get offset of verity hash within kernel
	rootOffset := getKernelVerityHashOffset(c)

	// extract verity hash from kernel
	ddcmd := fmt.Sprintf("dd if=/boot/coreos/vmlinuz-a skip=%d count=64 bs=1 status=none", rootOffset)
	hash := c.MustSSH(m, ddcmd)

	// find /usr dev
	usrdev := util.GetUsrDeviceNode(c, m)

	// figure out partition size for hash dev offset
	offset := c.MustSSH(m, "sudo e2size "+usrdev)
	offset = bytes.TrimSpace(offset)

	c.MustSSH(m, fmt.Sprintf("sudo veritysetup verify --verbose --hash-offset=%s %s %s %s", offset, usrdev, usrdev, hash))
}

// VerityCorruption asserts that a machine will fail to read a file from a
// verify filesystem whose blocks have been modified.
func VerityCorruption(c cluster.TestCluster) {
	m := c.Machines()[0]
	// skip unless we are actually using verity
	skipUnlessVerity(c, m)

	// assert that dm shows verity is in use and the device is valid (V)
	out := c.MustSSH(m, "sudo dmsetup --target verity status usr")

	fields := strings.Fields(string(out))
	if len(fields) != 4 {
		c.Fatalf("failed checking dmsetup status of usr: not enough fields in output (got %d)", len(fields))
	}

	if fields[3] != "V" {
		c.Fatalf("dmsetup status usr reports verity is not valid!")
	}

	// corrupt a file on disk and flush disk caches.
	// try setting NAME=CoreOS to NAME=LulzOS in /usr/lib/os-release

	// get usr device, probably vda3
	usrdev := util.GetUsrDeviceNode(c, m)

	// poke bytes into /usr/lib/os-release
	c.MustSSH(m, fmt.Sprintf(`echo NAME=LulzOS | sudo dd of=%s seek=$(expr $(sudo debugfs -R "blocks /lib/os-release" %s 2>/dev/null) \* 4096) bs=1 status=none`, usrdev, usrdev))

	// make sure we flush everything so cat has to go through to the device backing verity.
	c.MustSSH(m, "sudo /bin/sh -c 'sync; echo -n 3 >/proc/sys/vm/drop_caches'")

	// read the file back. if we can read it successfully, verity did not do its job.
	out, stderr, err := m.SSH("cat /usr/lib/os-release")
	if err == nil {
		c.Fatalf("verity did not prevent reading a corrupted file!")
	}
	for _, line := range strings.Split(string(stderr), "\n") {
		// cat is expected to fail on EIO; report other errors
		if line != "cat: /usr/lib/os-release: Input/output error" {
			c.Log(line)
		}
	}

	// assert that dm shows verity device is now corrupted (C)
	out = c.MustSSH(m, "sudo dmsetup --target verity status usr")

	fields = strings.Fields(string(out))
	if len(fields) != 4 {
		c.Fatalf("failed checking dmsetup status of usr: not enough fields in output (got %d)", len(fields))
	}

	if fields[3] != "C" {
		c.Fatalf("dmsetup status usr reports verity is valid after corruption!")
	}
}

// get offset of verity hash within kernel
func getKernelVerityHashOffset(c cluster.TestCluster) int {
	// assume ARM64 is only on QEMU for now
	if _, ok := c.Cluster.(*qemu.Cluster); ok && kola.QEMUOptions.Board == "arm64-usr" {
		return 512
	}
	return 64
}

func skipUnlessVerity(c cluster.TestCluster, m platform.Machine) {
	// figure out if we are actually using verity
	out, err := c.SSH(m, "sudo veritysetup status usr")
	if err != nil && bytes.Equal(out, []byte("/dev/mapper/usr is inactive.")) {
		// verity not in use, so skip.
		c.Skip("verity is not enabled")
	} else if err != nil {
		c.Fatalf("failed checking verity status: %s: %v", out, err)
	}
}
