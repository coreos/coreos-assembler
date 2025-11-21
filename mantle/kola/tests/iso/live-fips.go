package iso

import (
	"path/filepath"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:           testLiveFIPS,
		ClusterSize:   0,
		Name:          "iso.fips.uefi",
		Timeout:       installTimeoutMins * time.Minute,
		Distros:       []string{"rhcos"},
		Platforms:     []string{"qemu"},
		Architectures: []string{"x86_64", "aarch64"},
	})
}

// testLiveFIPS verifies that adding fips=1 to the ISO results in a FIPS mode system
func testLiveFIPS(c cluster.TestCluster) {
	opts := IsoTestOpts{enableUefi: true}

	var outdir string
	//var qc *qemu.Cluster
	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		outdir = pc.RuntimeConf().OutputDir
		//qc = pc
	default:
		c.Fatalf("Unsupported cluster type")
	}

	isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, config, err := newQemuBuilder(opts, outdir)
	if err != nil {
		c.Fatal(err)
	}
	defer builder.Close()
	if err := builder.AddIso(isopath, "", false); err != nil {
		c.Fatal(err)
	}

	// This is the core change under test - adding the `fips=1` kernel argument via
	// coreos-installer iso kargs modify should enter fips mode.
	// Removing this line should cause this test to fail.
	builder.AppendKernelArgs = "fips=1"

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err)
	}

	config.AddSystemdUnit("fips-verify.service", `
[Unit]
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=fips-signal-ok.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=grep 1 /proc/sys/crypto/fips_enabled
ExecStart=grep FIPS etc/crypto-policies/config

[Install]
RequiredBy=fips-signal-ok.service
`, conf.Enable)
	config.AddSystemdUnit("fips-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("fips-emergency-target.service", signalFailureUnit, conf.Enable)

	// Just for reliability, we'll run fully offline
	builder.Append("-net", "none")

	builder.SetConfig(config)
	mach, err := builder.Exec()
	if err != nil {
		c.Fatal(err)
	}
	defer mach.Destroy()

	err = awaitCompletion(c, mach, opts.console, outdir, completionChannel, nil, []string{liveOKSignal})
	if err != nil {
		c.Fatal(err)
	}
}
