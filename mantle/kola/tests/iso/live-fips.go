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

// These tests only run on RHCOS
var tests_RHCOS_uefi = []string{
	"iso-fips.uefi",
}

func init() {
	for _, testName := range tests_RHCOS_uefi {
		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				opts := getIsoTestOpts(testName)
				testLiveFIPS(c, opts)
			},
			ClusterSize:   0,
			Name:          "iso." + testName,
			Description:   "verifies that adding fips=1 to the ISO results in a FIPS mode system",
			Timeout:       installTimeoutMins * time.Minute,
			Distros:       []string{"rhcos"},
			Platforms:     []string{"qemu"},
			Architectures: []string{"x86_64", "aarch64"},
		})
	}
}

var fipsVerify = `[Unit]
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=fips-signal-ok.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=grep 1 /proc/sys/crypto/fips_enabled
ExecStart=grep FIPS etc/crypto-policies/config

[Install]
RequiredBy=fips-signal-ok.service`

func testLiveFIPS(c cluster.TestCluster, opts IsoTestOpts) {
	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}

	config, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	config.AddSystemdUnit("fips-verify.service", fipsVerify, conf.Enable)
	config.AddSystemdUnit("fips-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("fips-emergency-target.service", signalFailureUnit, conf.Enable)
	keys, err := qc.Keys()
	if err != nil {
		c.Fatal(err)
	}
	config.CopyKeys(keys)

	errchan := make(chan error)
	setupDisks := func(_ platform.MachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		if err := builder.AddIso(isopath, "", false); err != nil {
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

	options := platform.MachineOptions{AppendKernelArgs: "fips=1"}
	if opts.enableUefi {
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
