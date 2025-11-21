package iso

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
)

func init() {
	register.RegisterTest(isoTest("install", isoInstall, []string{"x86_64"}))

	register.RegisterTest(isoTest("offline-install", isoOfflineInstall, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.uefi", isoOfflineInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("offline-install.4k", isoOfflineInstall4k, []string{"s390x"}))
	register.RegisterTest(isoTest("offline-install.mpath", isoOfflineInstallMpath, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.mpath.uefi", isoOfflineInstallMpathUefi, []string{"aarch64"}))

	register.RegisterTest(isoTest("offline-install-fromram.4k", isoOfflineInstallFromRam4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("offline-install-fromram.4k.uefi", isoOfflineInstallFromRam4kUefi, []string{"x86_64", "aarch64"}))
	register.RegisterTest(isoTest("miniso-install", isoMinisoInstall, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.uefi", isoMinisoInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("miniso-install.4k", isoMinisoInstall4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.4k.uefi", isoMinisoInstall4kUefi, []string{"x86_64", "aarch64"}))

	register.RegisterTest(isoTest("miniso-install.nm", isoMinisoInstallNm, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.nm.uefi", isoMinisoInstallNmUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("miniso-install.4k.nm", isoMinisoInstall4kNm, []string{"ppc64le", "s390x"}))
	register.RegisterTest(isoTest("miniso-install.4k.nm.uefi", isoMinisoInstall4kNmUefi, []string{"x86_64", "aarch64"}))
}

func isoMinisoInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isMiniso: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isMiniso:   true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstall4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k: true,
		isMiniso: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstall4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:   true,
		enableUefi: true,
		isMiniso:   true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstallNm(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstallNmUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		isMiniso:     true,
		enableUefi:   true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstall4kNm(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		enable4k:     true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoMinisoInstall4kNmUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		enable4k:     true,
		enableUefi:   true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
		isOffline:  true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstall4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:  true,
		isOffline: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstallMpath(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableMultipath: true,
		isOffline:       true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstallMpathUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableMultipath: true,
		enableUefi:      true,
		isOffline:       true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstallFromRam4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:     true,
		isOffline:    true,
		isISOFromRAM: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
}

func isoOfflineInstallFromRam4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:     true,
		enableUefi:   true,
		isOffline:    true,
		isISOFromRAM: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveIso(c, opts)
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
