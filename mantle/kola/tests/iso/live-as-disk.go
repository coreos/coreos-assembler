package iso

import (
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
)

// The iso-as-disk tests are only supported in x86_64 because other
// architectures don't have the required hybrid partition table.
var tests_as_disk_x86_64 = []string{
	"iso-as-disk.bios",
	"iso-as-disk.uefi",
	"iso-as-disk.uefi-secure",
}

func init() {
	arch := coreosarch.CurrentRpmArch()
	if arch != "x86_64" {
		return
	}
	for _, testName := range tests_as_disk_x86_64 {
		opts := getIsoTestOpts(testName)
		// Set machine to boot from the ISO as disk
		opts.machineOpts.BootFrom = platform.BootFromISOAsDisk

		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				testLiveAsDisk(c, opts)
			},
			ClusterSize: 1,
			Name:        "iso." + testName,
			Description: "Verify ISO-as-disk boot works.",
			Timeout:     3 * time.Minute, // Just boots the ISO -> quick
			Flags:       []register.Flag{},
			Platforms:   []string{"qemu"},
			// Skip base checks (looks at journal for failures) until bootupd fix lands
			// https://github.com/coreos/fedora-coreos-tracker/issues/2136
			Tags:           []string{kola.SkipBaseChecksTag},
			MachineOptions: opts.machineOpts,
		})
	}
}

func testLiveAsDisk(c cluster.TestCluster, opts IsoTestOpts) {
	m := c.Machines()[0]
	// Verify we are on Live ISO
	c.RunCmdSync(m, "test -f /run/ostree-live")
	// And that no efi boot entry exists if on UEFI
	if opts.machineOpts.Firmware == "uefi" || opts.machineOpts.Firmware == "uefi-secure" {
		VerifyNoEfiBootEntry(c, m)
	}
}
