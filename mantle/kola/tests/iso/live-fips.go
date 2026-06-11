package iso

import (
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

// These tests only run on RHCOS
var tests_RHCOS_uefi = []string{
	"iso-fips.uefi",
}

func init() {
	for _, testName := range tests_RHCOS_uefi {
		opts := getIsoTestOpts(testName)

		register.RegisterTest(&register.Test{
			Run:           testLiveFIPS,
			ClusterSize:   1,
			Name:          "iso." + testName,
			Description:   "verifies that adding fips=1 to the ISO results in a FIPS mode system",
			Timeout:       3 * time.Minute, // Just boots the ISO -> quick
			Distros:       []string{"rhcos"},
			Platforms:     []string{"qemu"},
			Architectures: []string{"x86_64", "aarch64"},
			// Skip base checks (looks at journal for failures) until bootupd fix lands
			// https://github.com/coreos/fedora-coreos-tracker/issues/2136
			Tags: []string{kola.SkipBaseChecksTag, "reprovision"},
			MachineOptions: platform.MachineOptions{
				AppendKernelArgs: "fips=1",
				Firmware:         opts.firmware,
				BootFrom:         platform.BootFromISO,
			},
		})
	}
}

func testLiveFIPS(c cluster.TestCluster) {
	m := c.Machines()[0]
	// Verify we are on Live ISO
	c.RunCmdSync(m, "test -f /run/ostree-live")
	// And that FIPS is enabled
	c.RunCmdSync(m, "grep 1 /proc/sys/crypto/fips_enabled")
	c.RunCmdSync(m, "grep FIPS /etc/crypto-policies/config")
}
