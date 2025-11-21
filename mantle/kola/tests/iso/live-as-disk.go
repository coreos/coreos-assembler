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
	// The iso-as-disk tests are only supported in x86_64 because other
	// architectures don't have the required hybrid partition table.
	var tests_as_disk_x86_64 = []string{
		"iso-as-disk.bios",
		"iso-as-disk.uefi",
		"iso-as-disk.uefi-secure",
		"iso-as-disk.4k.uefi",
	}

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
