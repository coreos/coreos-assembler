package iso

import (
	"path/filepath"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
)

func init() {
	// The iso-as-disk tests are only supported in x86_64 because other
	// architectures don't have the required hybrid partition table.
	register.RegisterTest(isoTest("as-disk", isoAsDisk, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi", isoAsDiskUefi, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi-secure", isoAsDiskUefiSecure, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.4k.uefi", isoAsDisk4kUefi, []string{"x86_64"}))
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

func isoAsDisk4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:   true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	isoTestAsDisk(c, opts)
}

func isoTestAsDisk(c cluster.TestCluster, opts IsoTestOpts) {
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
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	if err := builder.AddIso(isopath, "", true); err != nil {
		c.Fatal(err)
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err)
	}

	config.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
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
