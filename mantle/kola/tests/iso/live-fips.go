package iso

import (
	"fmt"
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
	register.RegisterTest(&register.Test{
		Run:           testLiveFIPS,
		ClusterSize:   0,
		Name:          "iso.fips.uefi",
		Description:   "verifies that adding fips=1 to the ISO results in a FIPS mode system",
		Distros:       []string{"rhcos"},
		Platforms:     []string{"qemu"},
		Architectures: []string{"x86_64", "aarch64"},
	})
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

func testLiveFIPS(c cluster.TestCluster) {
	if err := EnsureLiveArtifactsExist(); err != nil {
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
	config.AddSystemdUnit("fips-verify.service", fipsVerify, conf.Enable)
	config.AddSystemdUnit("fips-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("fips-emergency-target.service", signalFailureUnit, conf.Enable)
	keys, err := qc.Keys()
	if err != nil {
		c.Fatal(err)
	}
	config.CopyKeys(keys)

	overrideFW := func(builder *platform.QemuBuilder) error {
		builder.Firmware = "uefi"
		// This is the core change under test - adding the `fips=1` kernel argument via
		// coreos-installer iso kargs modify should enter fips mode.
		// Removing this line should cause this test to fail.
		builder.AppendKernelArgs = "fips=1"
		return nil
	}

	errchan := make(chan error)
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		output, err := builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		// Read line in a goroutine and send errors to channel
		go func() {
			errchan <- CheckTestOutput(output, []string{liveOKSignal})
		}()

		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		return builder.AddIso(isopath, "", false)
	}

	callacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
	_, err = qc.NewMachineWithQemuOptionsAndBuilderCallbacks(config, platform.QemuMachineOptions{}, callacks)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	err = <-errchan
	if err != nil {
		c.Fatal(err)
	}
}
