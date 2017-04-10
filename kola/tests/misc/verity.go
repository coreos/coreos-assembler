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
	"github.com/coreos/mantle/platform/machine/qemu"
)

func init() {
	register.Register(&register.Test{
		Run:         VerityVerify,
		ClusterSize: 1,
		Name:        "coreos.verity.verify",
		Platforms:   []string{"qemu", "aws", "gce"},
		UserData:    `#cloud-config`,
	})
	register.Register(&register.Test{
		Run:         VerityCorruption,
		ClusterSize: 1,
		Name:        "coreos.verity.corruption",
		Platforms:   []string{"qemu", "aws", "gce"},
		UserData:    `#cloud-config`,
	})
}

// Verity verification tests.
// TODO(mischief): seems like a good candidate for kolet.

// VerityVerify asserts that the filesystem mounted on /usr matches the
// dm-verity hash that is embedded in the CoreOS kernel.
func VerityVerify(c cluster.TestCluster) {
	m := c.Machines()[0]

	// get offset of verity hash within kernel
	rootOffset := 64
	// assume ARM64 is only on QEMU for now
	if _, ok := c.Cluster.(*qemu.Cluster); ok && kola.QEMUOptions.Board == "arm64-usr" {
		rootOffset = 512
	}

	// extract verity hash from kernel
	ddcmd := fmt.Sprintf("dd if=/boot/coreos/vmlinuz-a skip=%d count=64 bs=1 2>/dev/null", rootOffset)
	hash, err := m.SSH(ddcmd)
	if err != nil {
		c.Fatalf("failed to extract verity hash from kernel: %v: %v", hash, err)
	}

	// find /usr dev
	usrdev, err := m.SSH("findmnt -no SOURCE /usr")
	if err != nil {
		c.Fatalf("failed to find device for /usr: %v: %v", usrdev, err)
	}

	// XXX: if the /usr dev is /dev/mapper/usr, we're on a verity enabled
	// image, so use dmsetup to find the real device.
	if strings.TrimSpace(string(usrdev)) == "/dev/mapper/usr" {
		usrdev, err = m.SSH("echo -n /dev/$(sudo dmsetup info --noheadings -Co blkdevs_used usr)")
		if err != nil {
			c.Fatalf("failed to find device for /usr: %v: %v", usrdev, err)
		}
	}

	// figure out partition size for hash dev offset
	offset, err := m.SSH("sudo e2size " + string(usrdev))
	if err != nil {
		c.Fatalf("failed to find /usr partition size: %v: %v", offset, err)
	}

	offset = bytes.TrimSpace(offset)
	veritycmd := fmt.Sprintf("sudo veritysetup verify --verbose --hash-offset=%s %s %s %s", offset, usrdev, usrdev, hash)

	verify, err := m.SSH(veritycmd)
	if err != nil {
		c.Fatalf("verity hash verification on %s failed: %v: %v", usrdev, verify, err)
	}
}

// VerityCorruption asserts that a machine will fail to read a file from a
// verify filesystem whose blocks have been modified.
func VerityCorruption(c cluster.TestCluster) {
	m := c.Machines()[0]
	// figure out if we are actually using verity
	out, err := m.SSH("sudo veritysetup status usr")
	if err != nil && bytes.Equal(out, []byte("/dev/mapper/usr is inactive.")) {
		// verity not in use, so skip.
		c.Skip("verity is not enabled")
	} else if err != nil {
		c.Fatalf("failed checking verity status: %s: %v", out, err)
	}

	// assert that dm shows verity is in use and the device is valid (V)
	out, err = m.SSH("sudo dmsetup --target verity status usr")
	if err != nil {
		c.Fatalf("failed checking dmsetup status of usr: %s: %v", out, err)
	}

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
	usrdev, err := m.SSH("echo /dev/$(sudo dmsetup info --noheadings -Co blkdevs_used usr)")
	if err != nil {
		c.Fatalf("failed getting /usr device from dmsetup: %s: %v", out, err)
	}

	// poke bytes into /usr/lib/os-release
	out, err = m.SSH(fmt.Sprintf(`echo NAME=LulzOS | sudo dd of=%s seek=$(expr $(sudo debugfs -R "blocks /lib/os-release" %s 2>/dev/null) \* 4096) bs=1 2>/dev/null`, usrdev, usrdev))
	if err != nil {
		c.Fatalf("failed overwriting disk block: %s: %v", out, err)
	}

	// make sure we flush everything so cat has to go through to the device backing verity.
	out, err = m.SSH("sudo /bin/sh -c 'sync; echo -n 3 >/proc/sys/vm/drop_caches'")
	if err != nil {
		c.Fatalf("failed dropping disk caches: %s: %v", out, err)
	}

	// read the file back. if we can read it successfully, verity did not do its job.
	out, err = m.SSH("cat /usr/lib/os-release")
	if err == nil {
		c.Fatalf("verity did not prevent reading a corrupted file!")
	}

	// assert that dm shows verity device is now corrupted (C)
	out, err = m.SSH("sudo dmsetup --target verity status usr")
	if err != nil {
		c.Fatalf("failed checking dmsetup status of usr: %s: %v", out, err)
	}

	fields = strings.Fields(string(out))
	if len(fields) != 4 {
		c.Fatalf("failed checking dmsetup status of usr: not enough fields in output (got %d)", len(fields))
	}

	if fields[3] != "C" {
		c.Fatalf("dmsetup status usr reports verity is valid after corruption!")
	}
}
