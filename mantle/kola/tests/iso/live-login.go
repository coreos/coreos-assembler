package iso

import (
	"fmt"
	"os"
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

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}

	butane := conf.Butane(`
variant: fcos
version: 1.1.0`)

	overrideFW := func(builder *platform.QemuBuilder) error {
		switch {
		case enableUefiSecure:
			builder.Firmware = "uefi-secure"
		case enableUefi:
			builder.Firmware = "uefi"
		}
		return nil
	}

	var isoCompletionOutput *os.File
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		err := builder.AddIso(isopath, "", false)
		if err != nil {
			return err
		}

		// https://github.com/coreos/fedora-coreos-config/blob/testing-devel/overlay.d/05core/usr/lib/systemd/system/coreos-liveiso-success.service
		isoCompletionOutput, err = builder.VirtioChannelRead("coreos.liveiso-success")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		return nil
	}

	callbacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
	_, err := qc.NewMachineWithQemuOptionsAndBuilderCallbacks(butane, platform.QemuMachineOptions{}, callbacks)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	errchan := make(chan error)
	go func() {
		errchan <- checkTestOutput(isoCompletionOutput, []string{"coreos-liveiso-success"})
	}()

	err = <-errchan
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
