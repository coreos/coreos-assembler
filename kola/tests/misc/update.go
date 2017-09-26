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

package misc

import (
	"fmt"
	"regexp"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         RebootIntoUSRB,
		ClusterSize: 1,
		Name:        "coreos.update.reboot",
	})
	register.Register(&register.Test{
		Run:         RecoverBadVerity,
		ClusterSize: 1,
		Name:        "coreos.update.badverity",
		Flags:       []register.Flag{register.NoEmergencyShellCheck},
	})
	register.Register(&register.Test{
		Run:         RecoverBadUsr,
		ClusterSize: 1,
		Name:        "coreos.update.badusr",
		Flags:       []register.Flag{register.NoEmergencyShellCheck},
	})
}

// Simulate update scenarios

// Check that we can reprioritize and boot into USR-B. This largely
// validates the other tests in this file.
func RebootIntoUSRB(c cluster.TestCluster) {
	m := c.Machines()[0]

	assertBootedUsr(c, m, "USR-A")

	runAsRoot(c, m, []string{
		// copy USR-A to USR-B
		"dd if=/dev/disk/by-partlabel/USR-A of=/dev/disk/by-partlabel/USR-B bs=10M status=none",

		// copy kernel
		"cp /boot/coreos/vmlinuz-a /boot/coreos/vmlinuz-b",
	})

	prioritizeUsr(c, m, "USR-B")
	if err := m.Reboot(); err != nil {
		c.Fatalf("couldn't reboot: %v", err)
	}
	assertBootedUsr(c, m, "USR-B")
}

// Verify that we reboot into the old image after the new image fails a
// verity check.
func RecoverBadVerity(c cluster.TestCluster) {
	m := c.Machines()[0]

	skipUnlessVerity(c, m)

	assertBootedUsr(c, m, "USR-A")

	runAsRoot(c, m, []string{
		// copy USR-A to USR-B
		"dd if=/dev/disk/by-partlabel/USR-A of=/dev/disk/by-partlabel/USR-B bs=10M status=none",

		// copy kernel
		"cp /boot/coreos/vmlinuz-a /boot/coreos/vmlinuz-b",

		// invalidate verity hash on B kernel
		fmt.Sprintf("dd of=/boot/coreos/vmlinuz-b bs=1 seek=%d count=64 conv=notrunc status=none <<<0000000000000000000000000000000000000000000000000000000000000000", getKernelVerityHashOffset(c)),
	})

	prioritizeUsr(c, m, "USR-B")
	rebootWithEmergencyShellTimeout(c, m)
	assertBootedUsr(c, m, "USR-A")
}

// Verify that we reboot into the old image when the new image is an
// unreasonable filesystem (an empty one) that passes verity.
func RecoverBadUsr(c cluster.TestCluster) {
	m := c.Machines()[0]

	assertBootedUsr(c, m, "USR-A")

	runAsRoot(c, m, []string{
		// create filesystem for USR-B
		"mkfs.ext4 -q -b 4096 /dev/disk/by-partlabel/USR-B 25600",
	})

	// create verity metadata for USR-B
	output, err := c.SSH(m, "sudo veritysetup format --hash=sha256 "+
		"--data-block-size 4096 --hash-block-size 4096 --data-blocks 25600 --hash-offset 104857600 "+
		"/dev/disk/by-partlabel/USR-B /dev/disk/by-partlabel/USR-B")
	if err != nil {
		c.Fatalf("Failed to run veritysetup: output %s, status: %v", output, err)
	}

	// extract root hash for USR-B
	match := regexp.MustCompile("\nRoot hash:\\s+([0-9a-f]+)").FindSubmatch(output)
	if match == nil {
		c.Fatalf("Couldn't obtain new root hash; output %s", output)
	}
	verityHash := match[1]

	runAsRoot(c, m, []string{
		// copy kernel
		"cp /boot/coreos/vmlinuz-a /boot/coreos/vmlinuz-b",

		// update verity hash on B kernel
		fmt.Sprintf("dd of=/boot/coreos/vmlinuz-b bs=1 seek=%d count=64 conv=notrunc status=none <<<%s", getKernelVerityHashOffset(c), verityHash),
	})

	prioritizeUsr(c, m, "USR-B")
	rebootWithEmergencyShellTimeout(c, m)
	assertBootedUsr(c, m, "USR-A")
}

// run commands as root
func runAsRoot(c cluster.TestCluster, m platform.Machine, commands []string) {
	for _, command := range commands {
		output, err := c.SSH(m, "sudo "+command)
		if err != nil {
			c.Fatalf("Failed to run %q: output %s, status: %v", command, output, err)
		}
	}
}

func assertBootedUsr(c cluster.TestCluster, m platform.Machine, usr string) {
	usrdev := getUsrDeviceNode(c, m)
	target, err := c.SSH(m, "readlink -f /dev/disk/by-partlabel/"+usr)
	if err != nil {
		c.Fatalf("Failed to readlink: output %s, status %v", target, err)
	}
	if usrdev != string(target) {
		c.Fatalf("Expected /usr to be %v (%s) but it is %v", usr, target, usrdev)
	}
}

func prioritizeUsr(c cluster.TestCluster, m platform.Machine, usr string) {
	runAsRoot(c, m, []string{
		"cgpt repair /dev/disk/by-partlabel/" + usr,
		"cgpt add -S0 -T1 /dev/disk/by-partlabel/" + usr,
		"cgpt prioritize /dev/disk/by-partlabel/" + usr,
	})
}

// reboot, waiting extra-long for the 5-minute emergency shell timeout
func rebootWithEmergencyShellTimeout(c cluster.TestCluster, m platform.Machine) {
	// reboot; wait extra 5 minutes; check machine
	// this defeats some of the machinery in m.Reboot()
	if err := platform.StartReboot(m); err != nil {
		c.Fatal(err)
	}
	time.Sleep(5 * time.Minute)
	if err := platform.CheckMachine(m); err != nil {
		c.Fatal(err)
	}
}
