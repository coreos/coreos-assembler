package iso

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

var (
	tests_pxe_x86_64 = []string{
		"pxe-offline-install.rootfs-appended.bios",
		"pxe-offline-install.4k.uefi",
		"pxe-online-install.bios",
		"pxe-online-install.4k.uefi",
	}
	tests_pxe_aarch64 = []string{
		"pxe-offline-install.uefi",
		"pxe-offline-install.rootfs-appended.4k.uefi",
		"pxe-online-install.uefi",
		"pxe-online-install.4k.uefi",
	}
	tests_pxe_ppc64le = []string{
		"pxe-online-install.rootfs-appended.ppcfw",
		"pxe-offline-install.4k.ppcfw",
	}
	tests_pxe_s390x = []string{
		"pxe-online-install.rootfs-appended.s390fw",
		"pxe-offline-install.s390fw",
	}
)

func getAllPxeTests() []string {
	arch := coreosarch.CurrentRpmArch()
	switch arch {
	case "x86_64":
		return tests_pxe_x86_64
	case "aarch64":
		return tests_pxe_aarch64
	case "ppc64le":
		return tests_pxe_ppc64le
	case "s390x":
		return tests_pxe_s390x
	default:
		return []string{}
	}
}

func init() {
	for _, testName := range getAllPxeTests() {
		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				opts := getIsoTestOpts(testName)
				testLivePXE(c, opts)
			},
			ClusterSize: 0,
			Name:        "iso." + testName,
			Description: "Verify PXE install works.",
			Timeout:     installTimeoutMins * time.Minute,
			Flags:       []register.Flag{},
			Platforms:   []string{"qemu"},
		})
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

func testLivePXE(c cluster.TestCluster, opts IsoTestOpts) {
	EnsureLiveArtifactsExist(c)

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}

	if opts.addNmKeyfile {
		c.Fatalf("--add-nm-keyfile not yet supported for PXE")
	}

	targetConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	keys, err := qc.Keys()
	if err != nil {
		c.Fatal(err)
	}
	targetConfig.CopyKeys(keys)
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)

	tempdir, err := os.MkdirTemp("/var/tmp", "mantle-pxe")
	if err != nil {
		c.Fatal(err)
	}
	defer os.RemoveAll(tempdir)

	pxe, server, err := createPXE(tempdir, opts)
	if err != nil {
		c.Fatal(errors.Wrapf(err, "setting up install"))
	}
	defer server.Close()

	initBuilder := func(o platform.MachineOptions, builder *platform.QemuBuilder) error {
		if err := qc.InitDefaultBuilder(o, builder); err != nil {
			return err
		}
		// save PXE config
		ignitionPath, err := builder.IgnitionPath()
		if err != nil {
			return err
		}
		liveConfig, err := getPXEConfig(opts.instInsecure, opts.isOffline)
		if err != nil {
			return err
		}
		if err := liveConfig.WriteFile(ignitionPath); err != nil {
			return err
		}
		if err := absSymlink(ignitionPath, filepath.Join(pxe.tftpdir, "pxe-live.ign")); err != nil {
			return err
		}
		// save target config
		targetpath := filepath.Join(filepath.Dir(ignitionPath), "pxe-target.ign")
		if err := targetConfig.WriteFile(targetpath); err != nil {
			return err
		}
		if err := absSymlink(targetpath, filepath.Join(pxe.tftpdir, "pxe-target.ign")); err != nil {
			return err
		}
		return nil
	}

	setupNet := func(o platform.MachineOptions, builder *platform.QemuBuilder) error {
		usernetdev := ""
		if pxe.tftpipaddr != "10.0.2.2" {
			usernetdev = "192.168.76.0/24,dhcpstart=192.168.76.9"
		}
		h := []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
		}
		builder.SetNetbootP(pxe.bootfile, pxe.tftpdir)
		builder.SetNetbootIndex(pxe.bootindex)
		builder.EnableUsermodeNetworking(h, usernetdev)
		return nil
	}

	errchan := make(chan error)
	var bootStartedOutput *os.File
	setupDisks := func(_ platform.MachineOptions, builder *platform.QemuBuilder) error {
		sectorSize := 0
		if opts.enable4k {
			sectorSize = 4096
		}
		disk := platform.Disk{
			Size:       "12G", // Arbitrary
			SectorSize: sectorSize,
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
		isoCompletionOutput, err := builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up testisocompletion virtio-serial channel")
		}
		go func() {
			errchan <- CheckTestOutput(isoCompletionOutput, []string{liveOKSignal, signalCompleteString})
		}()

		bootStartedOutput, err = builder.VirtioChannelRead("bootstarted")
		if err != nil {
			return errors.Wrap(err, "setting up bootstarted virtio-serial channel")
		}
		return nil
	}

	options := platform.MachineOptions{
		MinMemory: 4096,
		Firmware:  opts.firmware,
	}

	// increase the memory for pxe tests with appended rootfs in the initrd
	// we were bumping up into the 4GiB limit in RHCOS/c9s
	if opts.pxeAppendRootfs {
		options.MinMemory = 5120
	}

	builder := &qemu.MachineBuilder{
		InitBuilder:  initBuilder,
		SetupDisks:   setupDisks,
		SetupNetwork: setupNet,
	}
	qm, err := qc.NewMachineWithBuilder(nil, options, builder)
	if err != nil {
		c.Fatal(errors.Wrap(err, "unable to create test machine"))
	}
	inst := qc.Instance(qm)
	if inst == nil {
		c.Fatalf("Failed to get QemuInstance from machine")
	}

	//check for error when switching boot order
	go func() {
		if err := CheckTestOutput(bootStartedOutput, []string{bootStartedSignal}); err != nil {
			errchan <- err
			return
		}
		if err := inst.SwitchBootOrder(); err != nil {
			errchan <- errors.Wrapf(err, "switching boot order failed")
			return
		}
	}()

	if err := <-errchan; err != nil {
		c.Fatal(err)
	}
}

func getPXEConfig(insecure bool, offline bool) (*conf.Conf, error) {
	installerConfig := coreosInstallerConfig{
		Console:     []string{platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]},
		AppendKargs: renderCosaTestIsoDebugKargs(),
		Insecure:    insecure,
	}
	installerConfigData, err := yaml.Marshal(installerConfig)
	if err != nil {
		return nil, err
	}
	mode := 0644

	liveConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return nil, err
	}
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	if offline {
		contents := fmt.Sprintf(downloadCheck, kola.CosaBuild.Meta.OstreeVersion)
		liveConfig.AddSystemdUnit("coreos-installer-offline-check.service", contents, conf.Enable)
	}
	// XXX: https://github.com/coreos/coreos-installer/issues/1171
	if coreosarch.CurrentRpmArch() != "s390x" {
		liveConfig.AddFile("/etc/coreos/installer.d/mantle.yaml", string(installerConfigData), mode)
	}
	liveConfig.AddAutoLogin()
	liveConfig.AddSystemdUnit("boot-started.service", bootStartedUnit, conf.Enable)
	return liveConfig, nil
}

type PXE struct {
	tftpdir      string
	tftpipaddr   string
	boottype     string
	bootindex    string
	pxeimagepath string
	bootfile     string
}

func createPXE(tempdir string, opts IsoTestOpts) (*PXE, *http.Server, error) {
	kernel := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel.Path
	initramfs := kola.CosaBuild.Meta.BuildArtifacts.LiveInitramfs.Path
	rootfs := kola.CosaBuild.Meta.BuildArtifacts.LiveRootfs.Path
	builddir := kola.CosaBuild.Dir

	tftpdir := filepath.Join(tempdir, "tftp")
	if err := os.Mkdir(tftpdir, 0777); err != nil {
		return nil, nil, err
	}

	for _, name := range []string{kernel, initramfs, rootfs} {
		if err := absSymlink(filepath.Join(builddir, name), filepath.Join(tftpdir, name)); err != nil {
			return nil, nil, err
		}
	}

	if opts.pxeAppendRootfs {
		// replace the initramfs symlink with a concatenation of
		// the initramfs and rootfs
		initrd := filepath.Join(tftpdir, initramfs)
		if err := os.Remove(initrd); err != nil {
			return nil, nil, err
		}
		if err := cat(initrd, filepath.Join(builddir, initramfs), filepath.Join(builddir, rootfs)); err != nil {
			return nil, nil, err
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
		return nil, nil, errors.Wrapf(err, "setting up metal image")
	}

	pxe := &PXE{
		tftpdir: tftpdir,
	}
	if err := pxe.setupArchDefaults(opts); err != nil {
		return nil, nil, err
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, nil, err
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
			return nil, nil, err
		}
	case "grub":
		if err := pxe.configBootGrub(kargsStr); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.Errorf("Unhandled boottype %s", pxe.boottype)
	}

	server := startHTTPServer(listener, tftpdir)
	return pxe, server, nil
}

func (pxe *PXE) setupArchDefaults(opts IsoTestOpts) error {
	pxe.tftpipaddr = "192.168.76.2"
	switch coreosarch.CurrentRpmArch() {
	case "x86_64":
		if opts.firmware == "uefi" {
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
		pxe.bootfile = "/boot/grub2/grubaa64.efi"
		pxe.pxeimagepath = "/boot/efi/EFI/fedora/grubaa64.efi"
		pxe.bootindex = "1"
	case "ppc64le":
		pxe.boottype = "grub"
		pxe.bootfile = "/boot/grub2/powerpc-ieee1275/core.elf"
	case "s390x":
		pxe.boottype = "pxe"
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
