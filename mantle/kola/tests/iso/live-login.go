package iso

import (
	"fmt"
	"path/filepath"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
)

func init() {
	register.RegisterTest(isoTest("live-login", isoLiveLogin, []string{}))
	register.RegisterTest(isoTest("live-login.uefi", isoLiveLoginUefi, []string{"x86_64", "aarch64"}))
	register.RegisterTest(isoTest("live-login.uefi-secure", isoLiveLoginUefiSecure, []string{"x86_64", "aarch64"}))
}

func testLiveLogin(c cluster.TestCluster, enableUefi bool, enableUefiSecure bool) {
	if err := ensureLiveArtifactsExist(); err != nil {
		fmt.Println(err)
		return
	}
	butane := conf.Butane(`
variant: fcos
version: 1.1.0`)

	errchan := make(chan error)
	overrideFW := func(builder *platform.QemuBuilder) error {
		switch {
		case enableUefiSecure:
			builder.Firmware = "uefi-secure"
		case enableUefi:
			builder.Firmware = "uefi"
		}
		return nil
	}
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		// https://github.com/coreos/fedora-coreos-config/blob/testing-devel/overlay.d/05core/usr/lib/systemd/system/coreos-liveiso-success.service
		output, err := builder.VirtioChannelRead("coreos.liveiso-success")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		// Read line in a goroutine and send errors to channel
		go func() {
			errchan <- checkTestOutput(output, []string{"coreos-liveiso-success"})
		}()

		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		return builder.AddIso(isopath, "", false)
	}

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		callbacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
		_, err := pc.NewMachineWithQemuOptionsAndBuilderCallbacks(butane, platform.QemuMachineOptions{}, callbacks)
		if err != nil {
			c.Fatalf("Unable to create test machine: %v", err)
		}
	default:
		c.Fatalf("Unsupported cluster type")
	}

	err := <-errchan
	if err != nil {
		c.Fatal(err)
	}
}

func isoLiveLogin(c cluster.TestCluster) {
	testLiveLogin(c, false, false)
}

func isoLiveLoginUefi(c cluster.TestCluster) {
	testLiveLogin(c, true, false)
}
func isoLiveLoginUefiSecure(c cluster.TestCluster) {
	testLiveLogin(c, false, true)
}
