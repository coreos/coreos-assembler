package iso

import (
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
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
		var firmware string
		if strings.Contains(testName, "uefi-secure") {
			firmware = "uefi-secure"
		} else if strings.Contains(testName, "uefi") {
			firmware = "uefi"
		}

		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				testLiveLogin(c, firmware)
			},
			ClusterSize: 0,
			Name:        "iso." + testName,
			Description: "Verify ISO live login works.",
			Flags:       []register.Flag{},
			Platforms:   []string{"qemu"},
			// Skip base checks (looks at journal for failures) until bootupd fix lands
			// https://github.com/coreos/fedora-coreos-tracker/issues/2136
			Tags: []string{kola.SkipBaseChecksTag},
		})
	}
}

func testLiveLogin(c cluster.TestCluster, firmware string) {
	EnsureLiveArtifactsExist(c)

	butane := conf.Butane(`
variant: fcos
version: 1.1.0`)

	errchan := make(chan error)

	setupDisks := func(_ platform.MachineOptions, builder *platform.QemuBuilder) error {
		// https://github.com/coreos/fedora-coreos-config/blob/testing-devel/overlay.d/05core/usr/lib/systemd/system/coreos-liveiso-success.service
		output, err := builder.VirtioChannelRead("coreos.liveiso-success")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		// Read line in a goroutine and send errors to channel
		go func() {
			errchan <- CheckTestOutput(output, []string{"coreos-liveiso-success"})
		}()

		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
		return builder.AddIso(isopath, "", false)
	}

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		options := platform.MachineOptions{Firmware: firmware}
		builder := &qemu.MachineBuilder{
			SetupDisks: setupDisks,
		}
		_, err := pc.NewMachineWithBuilder(butane, options, builder)
		if err != nil {
			c.Fatalf("Unable to create test machine: %v", err)
		}
	default:
		c.Fatalf("Unsupported cluster type")
	}

	if err := <-errchan; err != nil {
		c.Fatal(err)
	}
}
