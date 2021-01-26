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
	"strings"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/system"
)

var (
	/* The FCCT config used for generating this ignition config:
	variant: fcos
	version: 1.3.0
	boot_device:
	  mirror:
	    devices:
	      - /dev/sda
	      - /dev/sdb
	      - /dev/sdc
	*/
	bootmirror = conf.Ignition(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"disks": [
			  {
				"device": "/dev/vda",
				"partitions": [
				  {
					"label": "bios-1",
					"sizeMiB": 1,
					"typeGuid": "21686148-6449-6E6F-744E-656564454649"
				  },
				  {
					"label": "esp-1",
					"sizeMiB": 127,
					"typeGuid": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
				  },
				  {
					"label": "boot-1",
					"sizeMiB": 384
				  },
				  {
					"label": "root-1"
				  }
				],
				"wipeTable": true
			  },
			  {
				"device": "/dev/vdb",
				"partitions": [
				  {
					"label": "bios-2",
					"sizeMiB": 1,
					"typeGuid": "21686148-6449-6E6F-744E-656564454649"
				  },
				  {
					"label": "esp-2",
					"sizeMiB": 127,
					"typeGuid": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
				  },
				  {
					"label": "boot-2",
					"sizeMiB": 384
				  },
				  {
					"label": "root-2"
				  }
				],
				"wipeTable": true
			  },
			  {
				"device": "/dev/vdc",
				"partitions": [
				  {
					"label": "bios-3",
					"sizeMiB": 1,
					"typeGuid": "21686148-6449-6E6F-744E-656564454649"
				  },
				  {
					"label": "esp-3",
					"sizeMiB": 127,
					"typeGuid": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
				  },
				  {
					"label": "boot-3",
					"sizeMiB": 384
				  },
				  {
					"label": "root-3"
				  }
				],
				"wipeTable": true
			  }
			],
			"filesystems": [
			 { 
				"device": "/dev/disk/by-partlabel/esp-1",
				"format": "vfat",
				"label": "esp-1",
				"wipeFilesystem": true
			  },
			  {
				"device": "/dev/disk/by-partlabel/esp-2",
				"format": "vfat",
				"label": "esp-2",
				"wipeFilesystem": true
			  },
			  {
				"device": "/dev/disk/by-partlabel/esp-3",
				"format": "vfat",
				"label": "esp-3",
				"wipeFilesystem": true
			  },
			  {
				"device": "/dev/md/md-boot",
				"format": "ext4",
				"label": "boot",
				"wipeFilesystem": true
			  },
			  {
				"device": "/dev/md/md-root",
				"format": "xfs",
				"label": "root",
				"wipeFilesystem": true
			  }
			],
			"raid": [
			  {
				"devices": [
				  "/dev/disk/by-partlabel/boot-1",
				  "/dev/disk/by-partlabel/boot-2",
				  "/dev/disk/by-partlabel/boot-3"
				],
				"level": "raid1",
				"name": "md-boot",
				"options": [
				  "--metadata=1.0"
				]
			  },
			  {
				"devices": [
				  "/dev/disk/by-partlabel/root-1",
				  "/dev/disk/by-partlabel/root-2",
				  "/dev/disk/by-partlabel/root-3"
				],
				"level": "raid1",
				"name": "md-root"
			  }
			]
		  }
	}`)

	/* The FCCT config used for generating this ignition config:
		variant: fcos
		version: 1.3.0
		boot_device:
		  luks:
	            tpm2: true
		  mirror:
		    devices:
		      - /dev/sda
		      - /dev/sdb
	*/
	bootmirrorluks = conf.Ignition(`{
			"ignition": {
			  "version": "3.2.0"
			},
			"storage": {
			  "disks": [
				{
				  "device": "/dev/vda",
				  "partitions": [
					{
					  "label": "bios-1",
					  "sizeMiB": 1,
					  "typeGuid": "21686148-6449-6E6F-744E-656564454649"
					},
					{
					  "label": "esp-1",
					  "sizeMiB": 127,
					  "typeGuid": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
					},
					{
					  "label": "boot-1",
					  "sizeMiB": 384
					},
					{
					  "label": "root-1"
					}
				  ],
				  "wipeTable": true
				},
				{
				  "device": "/dev/vdb",
				  "partitions": [
					{
					  "label": "bios-2",
					  "sizeMiB": 1,
					  "typeGuid": "21686148-6449-6E6F-744E-656564454649"
					},
					{
					  "label": "esp-2",
					  "sizeMiB": 127,
					  "typeGuid": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
					},
					{
					  "label": "boot-2",
					  "sizeMiB": 384
					},
					{
					  "label": "root-2"
					}
				  ],
				  "wipeTable": true
				}
			  ],
			  "filesystems": [
				{
				  "device": "/dev/disk/by-partlabel/esp-1",
				  "format": "vfat",
				  "label": "esp1",
				  "wipeFilesystem": true
				},
				{
					"device": "/dev/disk/by-partlabel/esp-2",
					"format": "vfat",
					"label": "esp2",
					"wipeFilesystem": true
				},
				{
				  "device": "/dev/md/md-boot",
				  "format": "ext4",
				  "label": "boot",
				  "wipeFilesystem": true
				},
				{
				  "device": "/dev/mapper/root",
				  "format": "xfs",
				  "label": "root",
				  "wipeFilesystem": true
				}
			  ],
			  "luks": [
				{
				  "clevis": {
					"tpm2": true
				  },
				  "device": "/dev/md/md-root",
				  "label": "luks-root",
				  "name": "root",
				  "wipeVolume": true
				}
			  ],
			  "raid": [
				{
				  "devices": [
					"/dev/disk/by-partlabel/boot-1",
					"/dev/disk/by-partlabel/boot-2"
				  ],
				  "level": "raid1",
				  "name": "md-boot",
				  "options": [
					"--metadata=1.0"
				  ]
				},
				{
				  "devices": [
					"/dev/disk/by-partlabel/root-1",
					"/dev/disk/by-partlabel/root-2"
				  ],
				  "level": "raid1",
				  "name": "md-root"
				}
			  ]
			}
		  }`)
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         runBootMirrorTest,
		ClusterSize: 0,
		Name:        `coreos.boot-mirror`,
		Platforms:   []string{"qemu-unpriv"},
		// skipping this test on UEFI until https://github.com/coreos/coreos-assembler/issues/2039
		// gets resolved.
		ExcludeFirmwares: []string{"uefi"},
		Tags:             []string{"boot-mirror", "raid1"},
		FailFast:         true,
	})
	register.RegisterTest(&register.Test{
		Run:         runBootMirrorLUKSTest,
		ClusterSize: 0,
		Name:        `coreos.boot-mirror.luks`,
		Platforms:   []string{"qemu-unpriv"},
		// aarch64 and ppc64le are added temporarily to avoid test failure until
		// we support specifying FCC directly in kola. For more information:
		// https://github.com/coreos/coreos-assembler/issues/2035
		// Also, TPM doesn't support s390x in qemu.
		ExcludeArchitectures: []string{"aarch64", "ppc64le", "s390x"},
		// skipping this test on UEFI until https://github.com/coreos/coreos-assembler/issues/2039
		// gets resolved.
		ExcludeFirmwares: []string{"uefi"},
		Tags:             []string{"boot-mirror", "luks", "raid1", "tpm2", kola.NeedsInternetTag},
		FailFast:         true,
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
	m, err = c.Cluster.(*unprivqemu.Cluster).NewMachineWithQemuOptions(bootmirror, options)
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
	m, err = c.Cluster.(*unprivqemu.Cluster).NewMachineWithQemuOptions(bootmirrorluks, options)
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
		fsTypeForBoot := c.MustSSH(m, "findmnt -nvr /boot -o FSTYPE")
		if strings.Compare(string(fsTypeForBoot), "ext4") != 0 {
			c.Fatalf("didn't match fstype for boot")
		}
		// Check that growpart didn't run
		c.MustSSH(m, "if [ -e /run/coreos-growpart.stamp ]; then exit 1; fi")
	})
}

func detachPrimaryBlockDevice(c cluster.TestCluster, m platform.Machine) {
	// Nuke primary block device and reboot the machine
	c.Run("detach-primary", func(c cluster.TestCluster) {
		if err := m.(platform.QEMUMachine).RemovePrimaryBlockDevice(); err != nil {
			c.Fatalf("failed to delete the first boot disk: %v", err)
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
		rootOutput := c.MustSSH(m, "sudo mdadm -Q --detail /dev/md/md-root")
		if !strings.Contains(string(rootOutput), "degraded") {
			c.Fatalf("didn't match the state of root raid device; expected degraded, found: %v", string(rootOutput))
		}
		bootOutput := c.MustSSH(m, "sudo mdadm -Q --detail /dev/md/md-boot")
		if !strings.Contains(string(bootOutput), "degraded") {
			c.Fatalf("didn't match the state of boot raid device; expected degraded, found: %v", string(bootOutput))
		}
		c.MustSSH(m, "grep root=UUID= /proc/cmdline")
		c.MustSSH(m, "grep rd.md.uuid= /proc/cmdline")
	})
}
