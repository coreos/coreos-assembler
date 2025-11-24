package iso

import (
	"path/filepath"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/pkg/errors"
)

// The iso-as-disk tests are only supported in x86_64 because other
// architectures don't have the required hybrid partition table.
var tests_as_disk_x86_64 = []string{
	"iso-as-disk.bios",
	"iso-as-disk.uefi",
	"iso-as-disk.uefi-secure",
}

func init() {
	for _, testName := range tests_as_disk_x86_64 {
		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				opts := getIsoTestOpts(testName)
				isoTestAsDisk(c, opts)
			},
			ClusterSize: 0,
			Name:        "iso." + testName,
			Description: "Verify ISO-as-disk install works.",
			Timeout:     installTimeoutMins * time.Minute,
			Flags:       []register.Flag{},
			Platforms:   []string{"qemu"},
		})
	}
}

func isoTestAsDisk(c cluster.TestCluster, opts IsoTestOpts) {
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
	keys, err := qc.Keys()
	if err != nil {
		c.Fatal(err)
	}
	config.CopyKeys(keys)

	errchan := make(chan error)
	setupDisks := func(_ platform.MachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		if err := builder.AddIso(isopath, "", true); err != nil {
			return err
		}
		completionChannel, err := builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}
		go func() {
			errchan <- CheckTestOutput(completionChannel, []string{liveOKSignal})
		}()
		return nil
	}

	options := platform.MachineOptions{}
	switch {
	case opts.enableUefiSecure:
		options.Firmware = "uefi-secure"
	case opts.enableUefi:
		options.Firmware = "uefi"
	}

	machineBuilder := &qemu.MachineBuilder{
		SetupDisks: setupDisks,
	}
	_, err = qc.NewMachineWithBuilder(config, options, machineBuilder)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	if err := <-errchan; err != nil {
		c.Fatal(err)
	}
}
