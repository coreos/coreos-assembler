package testiso

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const (
	installTimeoutMins = 12
	// https://github.com/coreos/fedora-coreos-config/pull/2544
	liveISOFromRAMKarg = "coreos.liveiso.fromram"
)

func isoTest(name string, run func(c cluster.TestCluster), arch []string) *register.Test {
	return &register.Test{
		Run:           run,
		ClusterSize:   0,
		Name:          "iso." + name,
		Timeout:       installTimeoutMins * time.Minute,
		Platforms:     []string{"qemu"},
		Architectures: arch,
	}
}

func init() {
	// The iso-as-disk tests are only supported in x86_64 because other
	// architectures don't have the required hybrid partition table.
	register.RegisterTest(isoTest("as-disk", isoAsDisk, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi", isoAsDiskUefi, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.uefi-secure", isoAsDiskUefiSecure, []string{"x86_64"}))
	register.RegisterTest(isoTest("as-disk.4k.uefi", isoAsDisk4kUefi, []string{"x86_64"}))

	register.RegisterTest(isoTest("install", isoInstall, []string{"x86_64"}))

	register.RegisterTest(isoTest("offline-install", isoOfflineInstall, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.uefi", isoOfflineInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("offline-install.4k", isoOfflineInstall4k, []string{"s390x"}))
	register.RegisterTest(isoTest("offline-install.mpath", isoOfflineInstallMpath, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.mpath.uefi", isoOfflineInstallMpathUefi, []string{"aarch64"}))

	register.RegisterTest(isoTest("offline-install-fromram.4k", isoOfflineInstallFromRam4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("offline-install-fromram.4k.uefi", isoOfflineInstallFromRam4kUefi, []string{"x86_64", "aarch64"}))

	// Those currently work only on x86, see: https://github.com/coreos/fedora-coreos-tracker/issues/1657
	register.RegisterTest(isoTest("offline-install-iscsi.ibft.uefi", isoOfflineInstallIscsiIbftUefi, []string{"x86_64"}))
	register.RegisterTest(isoTest("offline-install-iscsi.ibft-with-mpath", isoOfflineInstallIscsiIbftMpath, []string{"x86_64"}))
	register.RegisterTest(isoTest("offline-install-iscsi.manual", isoOfflineInstallIscsiManual, []string{"x86_64"}))

	register.RegisterTest(isoTest("miniso-install", isoMinisoInstall, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.uefi", isoMinisoInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("miniso-install.4k", isoMinisoInstall4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.4k.uefi", isoMinisoInstall4kUefi, []string{"x86_64", "aarch64"}))

	register.RegisterTest(isoTest("miniso-install.nm", isoMinisoInstallNm, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("miniso-install.nm.uefi", isoMinisoInstallNmUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("miniso-install.4k.nm", isoMinisoInstall4kNm, []string{"ppc64le", "s390x"}))
	register.RegisterTest(isoTest("miniso-install.4k.nm.uefi", isoMinisoInstall4kNmUefi, []string{"x86_64", "aarch64"}))

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

var liveOKSignal = "live-test-OK"
var liveSignalOKUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Live ISO Completion
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
[Install]
# for install tests
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`, liveOKSignal)

var signalCompleteString = "coreos-installer-test-OK"
var signalCompletionUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Completion
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=multi-user.target`, signalCompleteString)

var signalEmergencyString = "coreos-installer-test-entered-emergency-target"
var signalFailureUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Failure
Requires=dev-virtio\\x2dports-testisocompletion.device
DefaultDependencies=false
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=emergency.target`, signalEmergencyString)

var multipathedRoot = `[Unit]
Description=TestISO Verify Multipathed Root
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c 'lsblk -pno NAME "/dev/mapper/$(multipath -l -v 1)" | grep -qw "$(findmnt -nvr /sysroot -o SOURCE)"'
[Install]
RequiredBy=multi-user.target`

var checkNoIgnition = `
[Unit]
Description=TestISO Verify No Ignition Config
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
After=coreos-ignition-firstboot-complete.service
RequiresMountsFor=/boot
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '[ ! -e /boot/ignition ]'
[Install]
RequiredBy=multi-user.target`

// This test is broken. Please fix!
// https://github.com/coreos/coreos-assembler/issues/3554
var verifyNoEFIBootEntry = `
[Unit]
Description=TestISO Verify No EFI Boot Entry
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionPathExists=/sys/firmware/efi
Before=live-signal-ok.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '! efibootmgr -v | grep -E "(HD|CDROM)\("'
[Install]
# for install tests
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`

// Verify that the volume ID is the OS name. See also
// https://github.com/openshift/assisted-image-service/pull/477.
// This is the same as the LABEL of the block device for ISO9660. See
// https://github.com/util-linux/util-linux/blob/643bdae8e38055e36acf2963c3416de206081507/libblkid/src/superblocks/iso9660.c#L366-L377
var verifyIsoVolumeId = `
[Unit]
Description=Verify ISO Volume ID
OnFailure=emergency.target
OnFailureJobMode=isolate
# only if we're actually mounting the ISO
ConditionPathIsMountPoint=/run/media/iso
[Service]
Type=oneshot
RemainAfterExit=yes
# the backing device name is arch-dependent, but we know it's mounted on /run/media/iso
ExecStart=bash -c "[[ $(findmnt -no LABEL /run/media/iso) == %s-* ]]"
[Install]
RequiredBy=coreos-installer.target`

// Unit to check that /run/media/iso is not mounted when
// coreos.liveiso.fromram kernel argument is passed
var isoNotMountedUnit = `
[Unit]
Description=Verify ISO is not mounted when coreos.liveiso.fromram
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionKernelCommandLine=coreos.liveiso.fromram
[Service]
Type=oneshot
StandardOutput=kmsg+console
StandardError=kmsg+console
RemainAfterExit=yes
# Would like to use SuccessExitStatus but it doesn't support what
# we want: https://github.com/systemd/systemd/issues/10297#issuecomment-1672002635
ExecStart=bash -c "if mountpoint /run/media/iso 2>/dev/null; then exit 1; fi"
[Install]
RequiredBy=coreos-installer.target`

var nmConnectionId = "CoreOS DHCP"
var nmConnectionFile = "coreos-dhcp.nmconnection"
var nmConnection = fmt.Sprintf(`[connection]
id=%s
type=ethernet
# add wait-device-timeout here so we make sure NetworkManager-wait-online.service will
# wait for a device to be present before exiting. See
# https://github.com/coreos/fedora-coreos-tracker/issues/1275#issuecomment-1231605438
wait-device-timeout=20000

[ipv4]
method=auto
`, nmConnectionId)

var nmstateConfigFile = "/etc/nmstate/br-ex.yml"
var nmstateConfig = `interfaces:
 - name: br-ex
   type: linux-bridge
   state: up
   ipv4:
     enabled: false
   ipv6:
     enabled: false
   bridge:
     port: []
`

// This is used to verify *both* the live and the target system in the `--add-nm-keyfile` path.
var verifyNmKeyfile = fmt.Sprintf(`[Unit]
Description=TestISO Verify NM Keyfile Propagation
OnFailure=emergency.target
OnFailureJobMode=isolate
Wants=network-online.target
After=network-online.target
Before=live-signal-ok.service
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/journalctl -u nm-initrd --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/journalctl -u NetworkManager --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/grep "%[1]s" /etc/NetworkManager/system-connections/%[2]s
# Also verify nmstate config
ExecStart=/usr/bin/nmcli c show br-ex
[Install]
# for live system
RequiredBy=coreos-installer.target
# for target system
RequiredBy=multi-user.target`, nmConnectionId, nmConnectionFile)

type IsoTestOpts struct {
	// Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	instInsecure bool
	//	Flags().StringSliceVar(&pxeKernelArgs, "pxe-kargs", nil, "Additional kernel arguments for PXE")
	pxeKernelArgs []string
	// Flags().BoolVar(&console, "console", false, "Connect qemu console to terminal, turn off automatic initramfs failure checking")
	console          bool
	addNmKeyfile     bool
	enable4k         bool
	enableMultipath  bool
	isOffline        bool
	isISOFromRAM     bool
	isMiniso         bool
	enableUefi       bool
	enableUefiSecure bool
	enableIbft       bool
	manual           bool
	pxeAppendRootfs  bool
}

func (o *IsoTestOpts) SetInsecureOnDevBuild() {
	// Ignore signing verification by default when running with development build
	// https://github.com/coreos/fedora-coreos-tracker/issues/908
	if strings.Contains(kola.CosaBuild.Meta.BuildID, ".dev.") {
		o.instInsecure = true
		//fmt.Printf("Detected development build; disabling signature verification\n")
	}
}

func newBaseQemuBuilder(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, error) {
	builder := qemu.NewMetalQemuBuilderDefault()
	if opts.enableUefiSecure {
		builder.Firmware = "uefi-secure"
	} else if opts.enableUefi {
		builder.Firmware = "uefi"
	}

	if err := os.MkdirAll(outdir, 0755); err != nil {
		return nil, err
	}

	builder.InheritConsole = opts.console
	if !opts.console {
		builder.ConsoleFile = filepath.Join(outdir, "console.txt")
	}

	if kola.QEMUOptions.Memory != "" {
		parsedMem, err := strconv.ParseInt(kola.QEMUOptions.Memory, 10, 32)
		if err != nil {
			return nil, err
		}
		builder.MemoryMiB = int(parsedMem)
	}

	// increase the memory for pxe tests with appended rootfs in the initrd
	// we were bumping up into the 4GiB limit in RHCOS/c9s
	// pxe-offline-install.rootfs-appended.bios tests
	if opts.pxeAppendRootfs && builder.MemoryMiB < 5120 {
		builder.MemoryMiB = 5120
	}

	return builder, nil
}

func newQemuBuilder(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, err := newBaseQemuBuilder(opts, outdir)
	if err != nil {
		return nil, nil, err
	}

	config, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return nil, nil, err
	}

	err = forwardJournal(outdir, builder, config)
	if err != nil {
		return nil, nil, err
	}

	return builder, config, nil
}

func forwardJournal(outdir string, builder *platform.QemuBuilder, config *conf.Conf) error {
	journalPipe, err := builder.VirtioJournal(config, "")
	if err != nil {
		return err
	}
	journalOut, err := os.OpenFile(filepath.Join(outdir, "journal.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	go func() {
		_, err := io.Copy(journalOut, journalPipe)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	return nil
}

func newQemuBuilderWithDisk(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, config, err := newQemuBuilder(opts, outdir)

	if err != nil {
		return nil, nil, err
	}

	sectorSize := 0
	if opts.enable4k {
		sectorSize = 4096
	}

	disk := platform.Disk{
		Size:          "12G", // Arbitrary
		SectorSize:    sectorSize,
		MultiPathDisk: opts.enableMultipath,
	}

	//TBD: see if we can remove this and just use AddDisk and inject bootindex during startup
	if coreosarch.CurrentRpmArch() == "s390x" || coreosarch.CurrentRpmArch() == "aarch64" {
		// s390x and aarch64 need to use bootindex as they don't support boot once
		if err := builder.AddDisk(&disk); err != nil {
			return nil, nil, err
		}
	} else {
		if err := builder.AddPrimaryDisk(&disk); err != nil {
			return nil, nil, err
		}
	}

	return builder, config, nil
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

func isoOfflineInstallIscsiIbftUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
		isOffline:  true,
		enableIbft: true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
}

func isoOfflineInstallIscsiIbftMpath(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi:      true,
		isOffline:       true,
		enableMultipath: true,
		enableIbft:      true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
}

func isoOfflineInstallIscsiManual(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:  true,
		manual:     true,
		enableIbft: true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
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

	mach, err := inst.PXE(opts.pxeKernelArgs, liveConfig, targetConfig, opts.isOffline)
	if err != nil {
		c.Fatal(err)
	}
	qc.AddMach(mach)

	err = awaitCompletion(c, mach.Instance(), opts.console, outdir, completionChannel, mach.BootStartedErrorChannel(), []string{liveOKSignal, signalCompleteString})
	if err != nil {
		c.Fatal(err)
	}
}

//go:embed iscsi_butane_setup.yaml
var iscsi_butane_config string

// iscsi_butane_setup.yaml contains the full butane config but here is an overview of the setup
// 1 - Boot a live ISO with two extra 10G disks with labels "target" and "var"
//   - Format and mount `virtio-var` to /var
//
// 2 - target.container -> start an iscsi target, using quay.io/coreos-assembler/targetcli
// 3 - setup-targetcli.service calls /usr/local/bin/targetcli_script:
//   - instructs targetcli to serve /dev/disk/by-id/virtio-target as an iscsi target
//   - disables authentication
//   - verifies the iscsi service is active and reachable
//
// 4 - install-coreos-to-iscsi-target.service calls /usr/local/bin/install-coreos-iscsi:
//   - mount iscsi target
//   - run coreos-installer on the mounted block device
//   - unmount iscsi
//
// 5 - coreos-iscsi-vm.container -> start a coreos-assembler conainer:
//   - launch kola qemuexec instructing it to boot from an iPXE script
//     wich in turns mount the iscsi target and load kernel
//   - note the virtserial port device: we pass through the serial port
//     that was created by kola for test completion
//
// 6 - /var/nested-ign.json contains an ignition config:
//   - when the system is booted, write a success string to /dev/virtio-ports/testisocompletion
//   - as this serial device is mapped to the host serial device, the test concludes
func isoInstalliScsi(c cluster.TestCluster, opts IsoTestOpts) {
	var outdir string
	//var qc *qemu.Cluster
	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		outdir = pc.RuntimeConf().OutputDir
		//qc = pc
	default:
		c.Fatalf("Unsupported cluster type")
	}

	var butane string
	if opts.enableIbft && opts.enableMultipath {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1 --append-karg rd.multipath=default --append-karg root=/dev/disk/by-label/dm-mpath-root --append-karg rw")
	} else if opts.enableIbft {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1")
	} else if opts.manual {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg netroot=iscsi:10.0.2.15::::iqn.2024-05.com.coreos:0")
	}

	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		c.Fatalf("build %s is missing live artifacts", kola.CosaBuild.Meta.Name)
	}
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, err := newBaseQemuBuilder(opts, outdir)
	if err != nil {
		c.Fatal(err)
	}
	defer builder.Close()
	if err := builder.AddIso(isopath, "", false); err != nil {
		c.Fatal(err)
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err)
	}

	// Create a serial channel to read the logs from the nested VM
	nestedVmLogsChannel, err := builder.VirtioChannelRead("nestedvmlogs")
	if err != nil {
		c.Fatal(err)
	}

	// Create a file to write the contents of the serial channel into
	nestedVMConsole, err := os.OpenFile(filepath.Join(outdir, "nested_vm_console.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		c.Fatal(err)
	}

	go func() {
		_, err := io.Copy(nestedVMConsole, nestedVmLogsChannel)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	// empty disk to use as an iscsi target to install coreOS on and subseqently boot
	// Also add a 10G disk that we will mount on /var, to increase space available when pulling containers
	err = builder.AddDisksFromSpecs([]string{"10G:serial=target", "10G:serial=var"})
	if err != nil {
		c.Fatal(err)
	}

	// We need more memory to start another VM within !
	builder.MemoryMiB = 2048

	var iscsiTargetConfig = conf.Butane(butane)

	config, err := iscsiTargetConfig.Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	err = forwardJournal(outdir, builder, config)
	if err != nil {
		c.Fatal(err)
	}

	// Add a failure target to stop the test if something go wrong rather than waiting for the 10min timeout
	config.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	// enable network
	builder.EnableUsermodeNetworking([]platform.HostForwardPort{}, "")

	// keep auto-login enabled for easier debug when running console
	config.AddAutoLogin()

	builder.SetConfig(config)

	// Bind mount in the COSA rootfs into the VM so we can use it as a
	// read-only rootfs for quickly starting the container to kola
	// qemuexec the nested VM for the test. See resources/iscsi_butane_setup.yaml
	builder.MountHost("/", "/var/cosaroot", true)
	config.MountHost("/var/cosaroot", true)

	mach, err := builder.Exec()
	if err != nil {
		c.Fatal(err)
	}
	defer mach.Destroy()

	err = awaitCompletion(c, mach, opts.console, outdir, completionChannel, nil, []string{"iscsi-boot-ok"})
	if err != nil {
		c.Fatal(err)
	}
}

func awaitCompletion(c cluster.TestCluster, inst *platform.QemuInstance, console bool, outdir string, qchan *os.File, booterrchan chan error, expected []string) error {
	ctx := c.Context()

	errchan := make(chan error)
	go func() {
		timeout := (time.Duration(installTimeoutMins*(100+kola.Options.ExtendTimeoutPercent)) * time.Minute) / 100
		time.Sleep(timeout)
		errchan <- fmt.Errorf("timed out after %v", timeout)
	}()
	if !console {
		go func() {
			errBuf, err := inst.WaitIgnitionError(ctx)
			if err == nil {
				if errBuf != "" {
					c.Logf("entered emergency.target in initramfs")
					path := filepath.Join(outdir, "ignition-virtio-dump.txt")
					if err := os.WriteFile(path, []byte(errBuf), 0644); err != nil {
						c.Errorf("Failed to write journal: %v", err)
					}
					err = platform.ErrInitramfsEmergency
				}
			}
			if err != nil {
				errchan <- err
			}
		}()
	}
	go func() {
		err := inst.Wait()
		// only one Wait() gets process data, so also manually check for signal
		//plog.Debugf("qemu exited err=%v", err)
		if err == nil && inst.Signaled() {
			err = errors.New("process killed")
		}
		if err != nil {
			errchan <- errors.Wrapf(err, "QEMU unexpectedly exited while awaiting completion")
		}
		time.Sleep(1 * time.Minute)
		errchan <- fmt.Errorf("QEMU exited; timed out waiting for completion")
	}()
	go func() {
		r := bufio.NewReader(qchan)
		for _, exp := range expected {
			l, err := r.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// this may be from QEMU getting killed or exiting; wait a bit
					// to give a chance for .Wait() above to feed the channel with a
					// better error
					time.Sleep(1 * time.Second)
					errchan <- fmt.Errorf("Got EOF from completion channel, %s expected", exp)
				} else {
					errchan <- errors.Wrapf(err, "reading from completion channel")
				}
				return
			}
			line := strings.TrimSpace(l)
			if line != exp {
				errchan <- fmt.Errorf("Unexpected string from completion channel: %s expected: %s", line, exp)
				return
			}
		}
		// OK!
		errchan <- nil
	}()
	go func() {
		//check for error when switching boot order
		if booterrchan != nil {
			if err := <-booterrchan; err != nil {
				errchan <- err
			}
		}
	}()
	err := <-errchan
	if err == nil {
		// No error so far, check the console and journal files
		consoleFile := filepath.Join(outdir, "console.txt")
		journalFile := filepath.Join(outdir, "journal.txt")
		files := []string{consoleFile, journalFile}
		for _, file := range files {
			fileName := filepath.Base(file)
			// Check if the file exists
			_, err := os.Stat(file)
			if os.IsNotExist(err) {
				fmt.Printf("The file: %v does not exist\n", fileName)
				continue
			} else if err != nil {
				fmt.Println(err)
				return err
			}
			// Read the contents of the file
			fileContent, err := os.ReadFile(file)
			if err != nil {
				fmt.Println(err)
				return err
			}
			// Check for badness with CheckConsole
			warnOnly, badlines := kola.CheckConsole([]byte(fileContent), nil)
			if len(badlines) > 0 {
				for _, badline := range badlines {
					if warnOnly {
						c.Errorf("bad log line detected: %v", badline)
					} else {
						c.Logf("bad log line detected: %v", badline)
					}
				}
				if !warnOnly {
					err = fmt.Errorf("errors found in log files")
					return err
				}
			}
		}
	}
	return err
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
