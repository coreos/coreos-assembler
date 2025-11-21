package iso

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
)

var (
	tests_live_iso_x86_64 = []string{
		"iso-install.bios",
		"iso-offline-install.bios",
		"iso-offline-install.mpath.bios",
		"iso-offline-install-fromram.bios",
		"iso-offline-install-fromram.4k.uefi",
		"miniso-install.bios",
		"miniso-install.4k.uefi",
		"miniso-install.nm.bios",
		"miniso-install.4k.nm.uefi",
	}
	tests_live_iso_aarch64 = []string{
		"iso-offline-install.uefi",
		"iso-offline-install.mpath.uefi",
		"iso-offline-install-fromram.4k.uefi",
		"miniso-install.uefi",
		"miniso-install.4k.uefi",
		"miniso-install.nm.uefi",
		"miniso-install.4k.nm.uefi",
	}
	tests_live_iso_ppc64le = []string{
		"iso-offline-install.ppcfw",
		"iso-offline-install.mpath.ppcfw",
		"iso-offline-install-fromram.4k.ppcfw",
		"miniso-install.ppcfw",
		"miniso-install.4k.ppcfw",
		"miniso-install.nm.ppcfw",
		"miniso-install.4k.nm.ppcfw",
		"miniso-install.4k.nm.uefi",
	}
	tests_live_iso_s390x = []string{
		"iso-offline-install.s390fw",
		"iso-offline-install.4k.s390fw",
		"iso-offline-install.mpath.s390fw",
		"miniso-install.s390fw",
		"miniso-install.nm.s390fw",
		"miniso-install.4k.nm.s390fw",
		"miniso-install.4k.nm.uefi",
	}
)

func getAllLiveIsoTests() []string {
	arch := coreosarch.CurrentRpmArch()
	switch arch {
	case "x86_64":
		return tests_live_iso_x86_64
	case "aarch64":
		return tests_live_iso_aarch64
	case "ppc64le":
		return tests_live_iso_ppc64le
	case "s390x":
		return tests_live_iso_s390x
	default:
		return []string{}
	}
}

func init() {
	for _, testName := range getAllLiveIsoTests() {
		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				opts := getIsoTestOpts(testName)
				isoLiveIso(c, opts)
			},
			ClusterSize: 0,
			Name:        "iso." + testName,
			Description: "Verify ISO live install works.",
			Timeout:     installTimeoutMins * time.Minute,
			Flags:       []register.Flag{},
			Platforms:   []string{"qemu"},
		})
	}
}

func isoLiveIso(c cluster.TestCluster, opts IsoTestOpts) {
	var outdir string
	var qc *qemu.Cluster
	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		outdir = pc.RuntimeConf().OutputDir
		qc = pc
	default:
		c.Fatalf("Unsupported cluster type")
	}

	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		c.Fatalf("build %s is missing live artifacts", kola.CosaBuild.Meta.Name)
	}

	inst := qemu.Install{
		CosaBuild:     kola.CosaBuild,
		NmKeyfiles:    make(map[string]string),
		Insecure:      opts.instInsecure,
		Native4k:      opts.enable4k,
		MultiPathDisk: opts.enableMultipath,
	}

	tmpd, err := os.MkdirTemp("", "kola-iso.live")
	if err != nil {
		c.Fatal(err)
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		c.Fatal(err)
	}

	builder, virtioJournalConfig, err := newQemuBuilderWithDisk(opts, outdir)
	if err != nil {
		c.Fatal(err)
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err)
	}

	var isoKernelArgs []string
	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	liveConfig.AddSystemdUnit("iso-not-mounted-when-fromram.service", isoNotMountedUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	volumeIdUnitContents := fmt.Sprintf(verifyIsoVolumeId, kola.CosaBuild.Meta.Name)
	liveConfig.AddSystemdUnit("verify-iso-volume-id.service", volumeIdUnitContents, conf.Enable)

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)
	if inst.MultiPathDisk {
		targetConfig.AddSystemdUnit("coreos-test-installer-multipathed.service", multipathedRoot, conf.Enable)
	}

	if opts.addNmKeyfile {
		liveConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		targetConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		// NM keyfile via `iso network embed`
		inst.NmKeyfiles[nmConnectionFile] = nmConnection
		// nmstate config via live Ignition config, propagated via
		// --copy-network, which is enabled by inst.NmKeyfiles
		liveConfig.AddFile(nmstateConfigFile, nmstateConfig, 0644)
	}

	if opts.isISOFromRAM {
		isoKernelArgs = append(isoKernelArgs, liveISOFromRAMKarg)
	}

	mach, err := inst.InstallViaISOEmbed(isoKernelArgs, liveConfig, targetConfig, outdir, opts.isOffline, opts.isMiniso)
	if err != nil {
		c.Fatal(err)
	}
	qc.AddMach(mach)
	err = awaitCompletion(c, mach.Instance(), opts.console, outdir, completionChannel, mach.BootStartedErrorChannel(), []string{liveOKSignal, signalCompleteString})
	if err != nil {
		c.Fatal(err)
	}
}
