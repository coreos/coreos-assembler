package iso

import (
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
	register.RegisterTest(isoTest("pxe-online-install", isoPxeOnlineInstall, []string{"x86_64"}))
	register.RegisterTest(isoTest("pxe-online-install.uefi", isoPxeOnlineInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("pxe-online-install.4k.uefi", isoPxeOnlineInstall4kUefi, []string{"x86_64", "aarch64"}))
	register.RegisterTest(isoTest("pxe-online-install.rootfs-appended", isoPxeOnlineInstallRootfsAppended, []string{"ppc64le", "s390x"}))
	register.RegisterTest(isoTest("pxe-offline-install", isoPxeOfflineInstall, []string{"s390x"}))
	register.RegisterTest(isoTest("pxe-offline-install.uefi", isoPxeOfflineInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("pxe-offline-install.4k", isoPxeOfflineInstall4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("pxe-offline-install.4k.uefi", isoPxeOfflineInstall4kUefi, []string{"x86_64", "aarch64"}))
	register.RegisterTest(isoTest("pxe-offline-install.rootfs-appended", isoPxeOfflineInstallRootfsAppended, []string{"x86_64"}))
	register.RegisterTest(isoTest("pxe-offline-install.rootfs-appended.4k.uefi", isoPxeOfflineInstallRootfsAppended4kUefi, []string{"aarch64"}))
}

func isoPxeOnlineInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOnlineInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOnlineInstall4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:   true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOnlineInstallRootfsAppended(c cluster.TestCluster) {
	opts := IsoTestOpts{
		pxeAppendRootfs: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:  true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstall4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline: true,
		enable4k:  true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstall4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:  true,
		enable4k:   true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstallRootfsAppended(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:       true,
		pxeAppendRootfs: true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

func isoPxeOfflineInstallRootfsAppended4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:       true,
		pxeAppendRootfs: true,
		enable4k:        true,
		enableUefi:      true,
	}
	opts.SetInsecureOnDevBuild()
	testPXE(c, opts)
}

var downloadCheck = `[Unit]
Description=TestISO Verify CoreOS Installer Download
After=coreos-installer.service
Before=coreos-installer.target
[Service]
Type=oneshot
StandardOutput=kmsg+console
StandardError=kmsg+console
ExecStart=/bin/sh -c "journalctl -t coreos-installer-service | /usr/bin/awk '/[Dd]ownload/ {exit 1}'"
ExecStart=/bin/sh -c "/usr/bin/udevadm settle"
ExecStart=/bin/sh -c "/usr/bin/mount /dev/disk/by-label/root /mnt"
ExecStart=/bin/sh -c "/usr/bin/jq -er '.[\"build\"]? + .[\"version\"]? == \"%s\"' /mnt/.coreos-aleph-version.json"
[Install]
RequiredBy=coreos-installer.target
`

func testPXE(c cluster.TestCluster, opts IsoTestOpts) {
	var outdir string
	var qc *qemu.Cluster

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		outdir = pc.RuntimeConf().OutputDir
		qc = pc
	default:
		c.Fatalf("Unsupported cluster type")
	}

	if opts.addNmKeyfile {
		c.Fatal("--add-nm-keyfile not yet supported for PXE")
	}

	inst := qemu.Install{
		CosaBuild:       kola.CosaBuild,
		NmKeyfiles:      make(map[string]string),
		Insecure:        opts.instInsecure,
		Native4k:        opts.enable4k,
		MultiPathDisk:   opts.enableMultipath,
		PxeAppendRootfs: opts.pxeAppendRootfs,
	}

	tmpd, err := os.MkdirTemp("", "kola-iso.pxe")
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

	// increase the memory for pxe tests with appended rootfs in the initrd
	// we were bumping up into the 4GiB limit in RHCOS/c9s
	// pxe-offline-install.rootfs-appended.bios tests
	if inst.PxeAppendRootfs && builder.MemoryMiB < 5120 {
		builder.MemoryMiB = 5120
	}

	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err) // , "setting up virtio-serial channel")
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	if opts.isOffline {
		contents := fmt.Sprintf(downloadCheck, kola.CosaBuild.Meta.OstreeVersion)
		liveConfig.AddSystemdUnit("coreos-installer-offline-check.service", contents, conf.Enable)
	}

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)

	mach, err := inst.PXE(kola.QEMUOptions.PxeKernelArgs, liveConfig, targetConfig, opts.isOffline)
	if err != nil {
		c.Fatal(err)
	}
	qc.AddMach(mach)

	err = awaitCompletion(c, mach.Instance(), opts.console, outdir, completionChannel, mach.BootStartedErrorChannel(), []string{liveOKSignal, signalCompleteString})
	if err != nil {
		c.Fatal(err)
	}
}
