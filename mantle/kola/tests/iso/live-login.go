package iso

import (
	"context"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/util"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
)

var (
	tests_live_login_x86_64 = []string{
		"live-login.bios",
		"live-login.uefi",
		"live-login.uefi-secure",
	}
	tests_live_login_aarch64 = []string{
		"live-login.uefi",
	}
	tests_live_login_ppc64le = []string{
		"live-login",
	}
	tests_live_login_s390x = []string{
		"live-login",
	}
)

func getAllLiveLoginTests() []string {
	arch := coreosarch.CurrentRpmArch()
	switch arch {
	case "x86_64":
		return tests_live_login_x86_64
	case "aarch64":
		return tests_live_login_aarch64
	case "ppc64le":
		return tests_live_login_ppc64le
	case "s390x":
		return tests_live_login_s390x
	default:
		return []string{}
	}
}

func init() {
	for _, testName := range getAllLiveLoginTests() {
		opts := getIsoTestOpts(testName)
		// Set machine to boot from the ISO
		opts.machineOpts.BootFrom = platform.BootFromISO
		// Set machine to not pass an Ignition config since we want to
		// verify autologin works (which only triggers with no config)
		opts.machineOpts.NoIgnition = true

		register.RegisterTest(&register.Test{
			Run:         testLiveLogin,
			ClusterSize: 1,
			Name:        "iso." + testName,
			Description: "Verify ISO live login works.",
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

func testLiveLogin(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Wait for the automatic login prompt to appear in the console output
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := util.WaitForConsoleOutput(ctx, m.ConsolePath(), "login: core (automatic login)"); err != nil {
		c.Fatalf("Failed waiting for automatic login: %v", err)
	}
}
