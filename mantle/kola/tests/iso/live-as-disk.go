package iso

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/pkg/errors"
)

func init() {
	// The iso-as-disk tests are only supported in x86_64 because other
	// architectures don't have the required hybrid partition table.
	register.RegisterTest(isoTest("as-disk", isoAsDisk, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi", isoAsDiskUefi, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi-secure", isoAsDiskUefiSecure, []string{"x86_64"}))
}

func isoAsDisk(c cluster.TestCluster) {
	opts := IsoTestOpts{}
	opts.SetInsecureOnDevBuild()
	isoTestAsDisk(c, opts)
}

func isoAsDiskUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	isoTestAsDisk(c, opts)
}
func isoAsDiskUefiSecure(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefiSecure: true,
	}
	opts.SetInsecureOnDevBuild()
	isoTestAsDisk(c, opts)
}

func isoTestAsDisk(c cluster.TestCluster, opts IsoTestOpts) {
	if err := ensureLiveArtifactsExist(); err != nil {
		fmt.Println(err)
		return
	}

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}

	config, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	config.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)

	overrideFW := func(builder *platform.QemuBuilder) error {
		switch {
		case opts.enableUefiSecure:
			builder.Firmware = "uefi-secure"
		case opts.enableUefi:
			builder.Firmware = "uefi"
		}
		return nil
	}

	var isoCompletionOutput *os.File
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		if err := builder.AddIso(isopath, "", true); err != nil {
			return err
		}

		isoCompletionOutput, err = builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		return nil
	}

	extra := platform.QemuMachineOptions{}
	extra.SkipStartMachine = true
	callbacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
	qm, err := qc.NewMachineWithQemuOptionsAndBuilderCallbacks(config, extra, callbacks)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	errchan := make(chan error)
	go func() {
		errchan <- qm.IgnitionError()
	}()
	go func() {
		errchan <- checkTestOutput(isoCompletionOutput, []string{liveOKSignal})
	}()

	err = <-errchan
	if err != nil {
		c.Fatal(err)
	}
}
