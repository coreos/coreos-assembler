// Copyright 2020 Red Hat, Inc.
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
	"encoding/json"
	"strings"
	"time"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/system"
	ut "github.com/coreos/mantle/util"
)

var (
	bootmirror = conf.Butane(`
variant: fcos
version: 1.3.0
boot_device:
  layout: LAYOUT
  mirror:
    devices:
      - /dev/vda
      - /dev/vdb
      - /dev/vdc`)

	bootmirrorluks = conf.Butane(`
variant: fcos
version: 1.3.0
boot_device:
  layout: LAYOUT
  luks:
    tpm2: true
  mirror:
    devices:
      - /dev/vda
      - /dev/vdb`)
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         runBootMirrorTest,
		ClusterSize: 0,
		Name:        `coreos.boot-mirror`,
		Platforms:   []string{"qemu-unpriv"},
		// Can't mirror boot disk on s390x
		ExcludeArchitectures: []string{"s390x"},
		// skipping this test on UEFI until https://github.com/coreos/coreos-assembler/issues/2039
		// gets resolved.
		ExcludeFirmwares: []string{"uefi"},
		Tags:             []string{"boot-mirror", "raid1"},
		FailFast:         true,
		Timeout:          15 * time.Minute,
	})
	register.RegisterTest(&register.Test{
		Run:         runBootMirrorLUKSTest,
		ClusterSize: 0,
		Name:        `coreos.boot-mirror.luks`,
		Platforms:   []string{"qemu-unpriv"},
		// Can't mirror boot disk on s390x, and qemu s390x doesn't
		// support TPM
		ExcludeArchitectures: []string{"s390x"},
		// skipping this test on UEFI until https://github.com/coreos/coreos-assembler/issues/2039
		// gets resolved.
		ExcludeFirmwares: []string{"uefi"},
		Tags:             []string{"boot-mirror", "luks", "raid1", "tpm2", kola.NeedsInternetTag},
		FailFast:         true,
		Timeout:          15 * time.Minute,
	})
}

// runBootMirrorTest verifies if the boot-mirror RAID1
// flow works properly in both BIOS and UEFI modes.
func runBootMirrorTest(c cluster.TestCluster) {
	var m platform.Machine
	var err error
	options := platform.QemuMachineOptions{
		MachineOptions: platform.MachineOptions{
			AdditionalDisks: []string{"5120M", "5120M"},
			MinMemory:       4096,
		},
	}
	// FIXME: for QEMU tests kola currently assumes the host CPU architecture
	// matches the one under test
	userdata := bootmirror.Subst("LAYOUT", system.RpmArch())
	m, err = c.Cluster.(*unprivqemu.Cluster).NewMachineWithQemuOptions(userdata, options)
	if err != nil {
		c.Fatal(err)
	}
	rootOutput := c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-root")
	if !strings.Contains(string(rootOutput), "/dev/vda4") || !strings.Contains(string(rootOutput), "/dev/vdb4") || !strings.Contains(string(rootOutput), "/dev/vdc4") {
		c.Fatalf("root raid device missing; found devices: %v", string(rootOutput))
	}
	bootOutput := c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-boot")
	if !strings.Contains(string(bootOutput), "/dev/vda3") || !strings.Contains(string(bootOutput), "/dev/vdb3") || !strings.Contains(string(bootOutput), "/dev/vdc3") {
		c.Fatalf("boot raid device missing; found devices: %v", string(bootOutput))
	}
	// Check for root
	checkIfMountpointIsRaid(c, m, "/sysroot")
	fsTypeForRoot := c.MustSSH(m, "findmnt -nvr /sysroot -o FSTYPE")
	if strings.Compare(string(fsTypeForRoot), "xfs") != 0 {
		c.Fatalf("didn't match fstype for root")
	}
	bootMirrorSanityTest(c, m)

	detachPrimaryBlockDevice(c, m)
	// Check if there are two devices with the active raid
	rootOutput = c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-root")
	if strings.Contains(string(rootOutput), "/dev/vdc4") || !(strings.Contains(string(rootOutput), "/dev/vda4") && strings.Contains(string(rootOutput), "/dev/vdb4")) {
		c.Fatalf("found unexpected root raid device; expected devices: %v", string(rootOutput))
	}
	bootOutput = c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-boot")
	if strings.Contains(string(bootOutput), "/dev/vdc3") || !(strings.Contains(string(bootOutput), "/dev/vda3") && strings.Contains(string(bootOutput), "/dev/vdb3")) {
		c.Fatalf("found unexpected boot raid device; expected devices: %v", string(bootOutput))
	}
	verifyBootMirrorAfterReboot(c, m)
}

// runBootMirrorLUKSTest verifies if the boot-mirror+LUKS RAID1
// flow works properly in both BIOS and UEFI modes.
func runBootMirrorLUKSTest(c cluster.TestCluster) {
	var m platform.Machine
	var err error
	options := platform.QemuMachineOptions{
		MachineOptions: platform.MachineOptions{
			AdditionalDisks: []string{"5120M"},
			MinMemory:       4096,
		},
	}
	// FIXME: for QEMU tests kola currently assumes the host CPU architecture
	// matches the one under test
	userdata := bootmirrorluks.Subst("LAYOUT", system.RpmArch())
	m, err = c.Cluster.(*unprivqemu.Cluster).NewMachineWithQemuOptions(userdata, options)
	if err != nil {
		c.Fatal(err)
	}
	rootOutput := c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-root")
	if !strings.Contains(string(rootOutput), "/dev/vda4") || !strings.Contains(string(rootOutput), "/dev/vdb4") {
		c.Fatalf("root raid device missing; found devices: %v", string(rootOutput))
	}
	bootOutput := c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-boot")
	if !strings.Contains(string(bootOutput), "/dev/vda3") || !strings.Contains(string(bootOutput), "/dev/vdb3") {
		c.Fatalf("boot raid device missing; found devices: %v", string(bootOutput))
	}
	bootMirrorSanityTest(c, m)
	luksTPMTest(c, m, true)

	detachPrimaryBlockDevice(c, m)
	// Check if there's only one device with the active raid
	rootOutput = c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-root")
	if !strings.Contains(string(rootOutput), "/dev/vda4") || strings.Contains(string(rootOutput), "/dev/vdb4") {
		c.Fatalf("found unexpected root raid device; expected devices: %v", string(rootOutput))
	}
	bootOutput = c.MustSSH(m, "sudo mdadm --export --detail /dev/md/md-boot")
	if !strings.Contains(string(bootOutput), "/dev/vda3") || strings.Contains(string(bootOutput), "/dev/vdb3") {
		c.Fatalf("found unexpected boot raid device; expected devices: %v", string(bootOutput))
	}
	verifyBootMirrorAfterReboot(c, m)
	// Re-check luks device after rebooting a machine
	luksTPMTest(c, m, true)
}

func luksTPMTest(c cluster.TestCluster, m platform.Machine, tpm2 bool) {
	rootPart := "/dev/md/md-root"
	// hacky,  but needed for s390x because of gpt issue with naming on big endian systems: https://bugzilla.redhat.com/show_bug.cgi?id=1899990
	if system.RpmArch() == "s390x" {
		rootPart = "/dev/disk/by-id/virtio-primary-disk-part4"
	}
	var tangd util.TangServer
	util.LUKSSanityTest(c, tangd, m, true, false, rootPart)
}

func bootMirrorSanityTest(c cluster.TestCluster, m platform.Machine) {
	c.Run("sanity-check", func(c cluster.TestCluster) {
		// Check for boot
		checkIfMountpointIsRaid(c, m, "/boot")
		c.AssertCmdOutputContains(m, "findmnt -nvr /boot -o FSTYPE", "ext4")
		// Check that growpart didn't run
		c.RunCmdSync(m, "if [ -e /run/coreos-growpart.stamp ]; then exit 1; fi")
	})
}

func detachPrimaryBlockDevice(c cluster.TestCluster, m platform.Machine) {
	// Nuke primary block device and reboot the machine
	c.Run("detach-primary", func(c cluster.TestCluster) {
		if err := m.(platform.QEMUMachine).RemovePrimaryBlockDevice(); err != nil {
			c.Fatalf("failed to delete the first boot disk: %v", err)
		}
		// Check if we can still SSH into the machine. We've noticed sometimes
		// that after removing the primary device, we lose connectivity.
		if err := ut.Retry(5, 3*time.Second, func() error {
			_, err2 := platform.GetMachineBootId(m)
			return err2
		}); err != nil {
			c.Fatalf("Failed to retrieve boot ID: %v", err)
		}
		err := m.Reboot()
		if err != nil {
			c.Fatalf("Failed to reboot the machine: %v", err)
		}
	})
}

func verifyBootMirrorAfterReboot(c cluster.TestCluster, m platform.Machine) {
	c.Run("verify-fallback", func(c cluster.TestCluster) {
		// Check if the RAIDs are in degraded state
		c.AssertCmdOutputContains(m, "sudo mdadm -Q --detail /dev/md/md-root", "degraded")
		c.AssertCmdOutputContains(m, "sudo mdadm -Q --detail /dev/md/md-boot", "degraded")
		c.RunCmdSync(m, "grep root=UUID= /proc/cmdline")
		c.RunCmdSync(m, "grep rd.md.uuid= /proc/cmdline")
	})
}

type lsblkOutput struct {
	Blockdevices []blockdevice `json:"blockdevices"`
}

type blockdevice struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Mountpoint *string `json:"mountpoint"`
	// new lsblk outputs `mountpoints` instead of
	// `mountpoint`; we handle both
	Mountpoints []string      `json:"mountpoints"`
	Children    []blockdevice `json:"children"`
}

// checkIfMountpointIsRaid will check if a given machine has a device of type
// raid1 mounted at the given mountpoint. If it does not, the test is failed.
func checkIfMountpointIsRaid(c cluster.TestCluster, m platform.Machine, mountpoint string) {
	output := c.MustSSH(m, "lsblk --json")

	l := lsblkOutput{}
	err := json.Unmarshal(output, &l)
	if err != nil {
		c.Fatalf("couldn't unmarshal lsblk output: %v", err)
	}

	foundDevice := checkIfMountpointIsRaidWalker(c, l.Blockdevices, mountpoint)
	if !foundDevice {
		c.Fatalf("didn't find %q mountpoint in lsblk output", mountpoint)
	}
}

// checkIfMountpointIsRaidWalker will iterate over bs and recurse into its
// children, looking for a device mounted at / with type raid1. true is returned
// if such a device is found. The test is failed if a device of a different type
// is found to be mounted at /.
func checkIfMountpointIsRaidWalker(c cluster.TestCluster, bs []blockdevice, mountpoint string) bool {
	for _, b := range bs {
		if checkIfBlockdevHasMountPoint(b, mountpoint) {
			if b.Type != "raid1" {
				c.Fatalf("device %q is mounted at %q with type %q (was expecting raid1)", b.Name, mountpoint, b.Type)
			}
			return true
		}
		foundDevice := checkIfMountpointIsRaidWalker(c, b.Children, mountpoint)
		if foundDevice {
			return true
		}
	}
	return false
}

// checkIfBlockdevHasMountPoint checks if a given block device has the
// required mountpoint.
func checkIfBlockdevHasMountPoint(b blockdevice, mountpoint string) bool {
	if b.Mountpoint != nil && *b.Mountpoint == mountpoint {
		return true
	} else if len(b.Mountpoints) != 0 {
		for _, mnt := range b.Mountpoints {
			if mnt != "" && mnt == mountpoint {
				return true
			}
		}
	}
	return false
}
