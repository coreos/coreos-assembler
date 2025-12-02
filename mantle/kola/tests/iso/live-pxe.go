package iso

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
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
	if err := ensureLiveArtifactsExist(); err != nil {
		fmt.Println(err)
		return
	}
	if opts.addNmKeyfile {
		c.Fatal("--add-nm-keyfile not yet supported for PXE")
	}

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}
	if opts.enable4k {
		qc.EnforceNative4k()
	}

	installerConfig := CoreosInstallerConfig{
		Console:     []string{platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]},
		AppendKargs: renderCosaTestIsoDebugKargs(),
		Insecure:    opts.instInsecure,
	}

	installerConfigData, err := yaml.Marshal(installerConfig)
	if err != nil {
		c.Fatal(err)
	}
	mode := 0644

	liveConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	if opts.isOffline {
		contents := fmt.Sprintf(downloadCheck, kola.CosaBuild.Meta.OstreeVersion)
		liveConfig.AddSystemdUnit("coreos-installer-offline-check.service", contents, conf.Enable)
	}
	// XXX: https://github.com/coreos/coreos-installer/issues/1171
	if coreosarch.CurrentRpmArch() != "s390x" {
		liveConfig.AddFile("/etc/coreos/installer.d/mantle.yaml", string(installerConfigData), mode)
	}
	liveConfig.AddAutoLogin()
	liveConfig.AddSystemdUnit("boot-started.service", bootStartedUnit, conf.Enable)

	targetConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)

	tempdir, err := os.MkdirTemp("/var/tmp", "mantle-pxe")
	if err != nil {
		c.Fatal(err)
	}
	defer func() {
		os.RemoveAll(tempdir)
	}()

	pxe, err := createPXE(c.Context(), tempdir, opts)
	if err != nil {
		c.Fatal(errors.Wrapf(err, "setting up install"))
	}

	overrideFW := func(builder *platform.QemuBuilder) error {
		if opts.enableUefi {
			builder.Firmware = "uefi"
		}
		// increase the memory for pxe tests with appended rootfs in the initrd
		// we were bumping up into the 4GiB limit in RHCOS/c9s
		builder.MemoryMiB = 4096
		if opts.pxeAppendRootfs {
			builder.MemoryMiB = 5120
		}

		if err := absSymlink(builder.ConfigFile, filepath.Join(pxe.tftpdir, "pxe-live.ign")); err != nil {
			return err
		}

		targetpath := filepath.Join(filepath.Dir(builder.ConfigFile), "pxe-target.ign")
		if err := targetConfig.WriteFile(targetpath); err != nil {
			return err
		}
		if err := absSymlink(targetpath, filepath.Join(pxe.tftpdir, "pxe-target.ign")); err != nil {
			return err
		}
		// don't attach config to VM
		builder.ConfigFile = ""
		return nil
	}

	setupNet := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		netdev := fmt.Sprintf("%s,netdev=mynet0,mac=52:54:00:12:34:56", pxe.networkdevice)
		if pxe.bootindex == "" {
			builder.Append("-boot", "once=n")
		} else {
			netdev += fmt.Sprintf(",bootindex=%s", pxe.bootindex)
		}
		builder.Append("-device", netdev)
		usernetdev := fmt.Sprintf("user,id=mynet0,tftp=%s,bootfile=%s", pxe.tftpdir, pxe.bootfile)
		if pxe.tftpipaddr != "10.0.2.2" {
			usernetdev += ",net=192.168.76.0/24,dhcpstart=192.168.76.9"
		}
		builder.Append("-netdev", usernetdev)
		return nil
	}

	var isoCompletionOutput *os.File
	var bootStartedOutput *os.File
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
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
				return err
			}
		} else {
			if err := builder.AddPrimaryDisk(&disk); err != nil {
				return err
			}
		}
		isoCompletionOutput, err = builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up testisocompletion virtio-serial channel")
		}
		bootStartedOutput, err = builder.VirtioChannelRead("bootstarted")
		if err != nil {
			return errors.Wrap(err, "setting up bootstarted virtio-serial channel")
		}
		return nil
	}

	extra := platform.QemuMachineOptions{}
	extra.SkipStartMachine = true
	callbacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, SetupNetwork: setupNet, OverrideDefaults: overrideFW}
	qm, err := qc.NewMachineWithQemuOptionsAndBuilderCallbacks(liveConfig, extra, callbacks)
	if err != nil {
		c.Fatal(errors.Wrap(err, "unable to create test machine"))
	}

	errchan := make(chan error)
	go func() {
		errchan <- checkTestOutput(isoCompletionOutput, []string{liveOKSignal, signalCompleteString})
	}()

	//check for error when switching boot order
	go func() {
		if err := checkTestOutput(bootStartedOutput, []string{bootStartedSignal}); err != nil {
			errchan <- err
			return
		}
		if err := qc.Instance(qm).SwitchBootOrder(); err != nil {
			errchan <- errors.Wrapf(err, "switching boot order failed")
			return
		}
	}()

	err = <-errchan
	if err != nil {
		c.Fatal(err)
	}
}

type PXE struct {
	tftpdir       string
	tftpipaddr    string
	boottype      string
	networkdevice string
	bootindex     string
	pxeimagepath  string
	bootfile      string
}

func createPXE(ctx context.Context, tempdir string, opts IsoTestOpts) (*PXE, error) {
	kernel := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel.Path
	initramfs := kola.CosaBuild.Meta.BuildArtifacts.LiveInitramfs.Path
	rootfs := kola.CosaBuild.Meta.BuildArtifacts.LiveRootfs.Path
	builddir := kola.CosaBuild.Dir

	tftpdir := filepath.Join(tempdir, "tftp")
	if err := os.Mkdir(tftpdir, 0777); err != nil {
		return nil, err
	}

	for _, name := range []string{kernel, initramfs, rootfs} {
		if err := absSymlink(filepath.Join(builddir, name), filepath.Join(tftpdir, name)); err != nil {
			return nil, err
		}
	}

	if opts.pxeAppendRootfs {
		// replace the initramfs symlink with a concatenation of
		// the initramfs and rootfs
		initrd := filepath.Join(tftpdir, initramfs)
		if err := os.Remove(initrd); err != nil {
			return nil, err
		}
		if err := cat(initrd, filepath.Join(builddir, initramfs), filepath.Join(builddir, rootfs)); err != nil {
			return nil, err
		}
	}

	var metalimg string
	if opts.enable4k {
		metalimg = kola.CosaBuild.Meta.BuildArtifacts.Metal4KNative.Path
	} else {
		metalimg = kola.CosaBuild.Meta.BuildArtifacts.Metal.Path
	}
	metalname, err := setupMetalImage(builddir, metalimg, tftpdir)
	if err != nil {
		return nil, errors.Wrapf(err, "setting up metal image")
	}

	pxe := &PXE{
		tftpdir: tftpdir,
	}
	if err := pxe.setupArchDefaults(opts); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	baseurl := fmt.Sprintf("http://%s:%d", pxe.tftpipaddr, port)

	kargs := renderCosaTestIsoDebugKargs()
	kargs = append(kargs, renderBaseKargs()...)
	kargs = append(kargs, kola.QEMUOptions.PxeKernelArgs...)
	kargs = append(kargs, fmt.Sprintf("ignition.config.url=%s/pxe-live.ign", baseurl))
	kargs = append(kargs, renderInstallKargs(baseurl, metalname, opts)...)
	if rootfs != "" && !opts.pxeAppendRootfs {
		kargs = append(kargs, fmt.Sprintf("coreos.live.rootfs_url=%s/%s", baseurl, rootfs))
	}
	kargsStr := strings.Join(kargs, " ")

	switch pxe.boottype {
	case "pxe":
		if err := pxe.configBootPxe(kargsStr); err != nil {
			return nil, err
		}
	case "grub":
		if err := pxe.configBootGrub(kargsStr); err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("Unhandled boottype %s", pxe.boottype)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(tftpdir)))
	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http serve: %v", err)
		}
	}()

	// stop server when ctx is canceled
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	return pxe, nil
}

func (pxe *PXE) setupArchDefaults(opts IsoTestOpts) error {
	pxe.tftpipaddr = "192.168.76.2"
	switch coreosarch.CurrentRpmArch() {
	case "x86_64":
		pxe.networkdevice = "e1000"
		if opts.enableUefi {
			pxe.boottype = "grub"
			pxe.bootfile = "/boot/grub2/grubx64.efi"
			pxe.pxeimagepath = "/boot/efi/EFI/fedora/grubx64.efi"
			// Choose bootindex=2. First boot the hard drive won't
			// have an OS and will fall through to bootindex 2 (net)
			pxe.bootindex = "2"
		} else {
			pxe.boottype = "pxe"
			pxe.pxeimagepath = "/usr/share/syslinux/"
		}
	case "aarch64":
		pxe.boottype = "grub"
		pxe.networkdevice = "virtio-net-pci"
		pxe.bootfile = "/boot/grub2/grubaa64.efi"
		pxe.pxeimagepath = "/boot/efi/EFI/fedora/grubaa64.efi"
		pxe.bootindex = "1"
	case "ppc64le":
		pxe.boottype = "grub"
		pxe.networkdevice = "virtio-net-pci"
		pxe.bootfile = "/boot/grub2/powerpc-ieee1275/core.elf"
	case "s390x":
		pxe.boottype = "pxe"
		pxe.networkdevice = "virtio-net-ccw"
		pxe.bootindex = "1"
		pxe.tftpipaddr = "10.0.2.2"
	default:
		return fmt.Errorf("unsupported arch %s", coreosarch.CurrentRpmArch())
	}
	return nil
}

func (pxe *PXE) configBootPxe(kargs string) error {
	kernel := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel.Path
	initramfs := kola.CosaBuild.Meta.BuildArtifacts.LiveInitramfs.Path

	pxeconfigdir := filepath.Join(pxe.tftpdir, "pxelinux.cfg")
	if err := os.Mkdir(pxeconfigdir, 0777); err != nil {
		return errors.Wrapf(err, "creating dir %s", pxeconfigdir)
	}
	pxeimages := []string{"pxelinux.0", "ldlinux.c32"}
	pxeconfig := []byte(fmt.Sprintf(`
DEFAULT pxeboot
TIMEOUT 20
PROMPT 0
LABEL pxeboot
	KERNEL %s
	APPEND initrd=%s %s
`, kernel, initramfs, kargs))
	if coreosarch.CurrentRpmArch() == "s390x" {
		pxeconfig = []byte(kargs)
	}
	pxeconfig_path := filepath.Join(pxeconfigdir, "default")
	if err := os.WriteFile(pxeconfig_path, pxeconfig, 0777); err != nil {
		return errors.Wrapf(err, "writing file %s", pxeconfig_path)
	}

	// this is only for s390x where the pxe image has to be created;
	// s390 doesn't seem to have a pre-created pxe image although have to check on this
	if pxe.pxeimagepath == "" {
		kernelpath := filepath.Join(pxe.tftpdir, kernel)
		initrdpath := filepath.Join(pxe.tftpdir, initramfs)
		err := exec.Command("/usr/bin/mk-s390image", kernelpath, "-r", initrdpath,
			"-p", filepath.Join(pxeconfigdir, "default"), filepath.Join(pxe.tftpdir, pxeimages[0])).Run()
		if err != nil {
			return errors.Wrap(err, "running mk-s390image")
		}
	} else {
		for _, img := range pxeimages {
			srcpath := filepath.Join("/usr/share/syslinux", img)
			cp_cmd := exec.Command("/usr/lib/coreos-assembler/cp-reflink", srcpath, pxe.tftpdir)
			cp_cmd.Stderr = os.Stderr
			if err := cp_cmd.Run(); err != nil {
				return errors.Wrapf(err, "running cp-reflink %s %s", srcpath, pxe.tftpdir)
			}
		}
	}
	pxe.bootfile = "/" + pxeimages[0]
	return nil
}

func (pxe *PXE) configBootGrub(kargs string) error {
	kernel := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel.Path
	initramfs := kola.CosaBuild.Meta.BuildArtifacts.LiveInitramfs.Path

	grub2_mknetdir_cmd := exec.Command("grub2-mknetdir", "--net-directory="+pxe.tftpdir)
	grub2_mknetdir_cmd.Stderr = os.Stderr
	if err := grub2_mknetdir_cmd.Run(); err != nil {
		return errors.Wrap(err, "running grub2-mknetdir")
	}
	if pxe.pxeimagepath != "" {
		dstpath := filepath.Join(pxe.tftpdir, "boot/grub2")
		cp_cmd := exec.Command("/usr/lib/coreos-assembler/cp-reflink", pxe.pxeimagepath, dstpath)
		cp_cmd.Stderr = os.Stderr
		if err := cp_cmd.Run(); err != nil {
			return errors.Wrapf(err, "running cp-reflink %s %s", pxe.pxeimagepath, dstpath)
		}
	}
	if err := os.WriteFile(filepath.Join(pxe.tftpdir, "boot/grub2/grub.cfg"), []byte(fmt.Sprintf(`
default=0
timeout=1
menuentry "CoreOS (BIOS/UEFI)" {
	echo "Loading kernel"
	linux /%s %s
	echo "Loading initrd"
	initrd %s
}`, kernel, kargs, initramfs)), 0777); err != nil {
		return errors.Wrap(err, "writing grub.cfg")
	}
	return nil
}

func renderBaseKargs() []string {
	baseKargs := []string{"rd.neednet=1", "ip=dhcp", "ignition.firstboot", "ignition.platform.id=metal"}
	return append(baseKargs, fmt.Sprintf("console=%s", platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]))
}

func renderInstallKargs(baseurl string, metalname string, opts IsoTestOpts) []string {
	args := []string{"coreos.inst.install_dev=/dev/vda",
		fmt.Sprintf("coreos.inst.ignition_url=%s/pxe-target.ign", baseurl)}
	if !opts.isOffline {
		args = append(args, fmt.Sprintf("coreos.inst.image_url=%s/%s", baseurl, metalname))
	}
	// FIXME - ship signatures by default too
	if opts.instInsecure {
		args = append(args, "coreos.inst.insecure")
	}
	return args
}
