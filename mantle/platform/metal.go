// Copyright 2020 Red Hat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package platform

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
)

const (
	// defaultQemuHostIPv4 is documented in `man qemu-kvm`, under the `-netdev` option
	defaultQemuHostIPv4 = "10.0.2.2"

	// rebootUnit is a copy of the system one without the ConditionPathExists
	rebootUnit = `[Unit]
	Description=Reboot after CoreOS Installer
	After=coreos-installer.service
	Requires=coreos-installer.service
	OnFailure=emergency.target
	OnFailureJobMode=replace-irreversibly
	
	[Service]
	Type=simple
	ExecStart=/usr/bin/systemctl --no-block reboot
	StandardOutput=kmsg+console
	StandardError=kmsg+console
	[Install]
	WantedBy=multi-user.target
`
)

// TODO derive this from docs, or perhaps include kargs in cosa metadata?
var baseKargs = []string{"rd.neednet=1", "ip=dhcp", "ignition.firstboot", "ignition.platform.id=metal"}

var (
	// TODO expose this as an API that can be used by cosa too
	consoleKernelArgument = map[string]string{
		"x86_64":  "ttyS0",
		"ppc64le": "hvc0",
		"aarch64": "ttyAMA0",
		"s390x":   "ttysclp0",
	}
)

// NewMetalQemuBuilderDefault returns a QEMU builder instance with some
// defaults set up for bare metal.
func NewMetalQemuBuilderDefault() *QemuBuilder {
	builder := NewBuilder()
	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	builder.Memory = 4096
	if system.RpmArch() == "s390x" {
		// After some trial and error looks like we need at least 10G on s390x
		// Recorded an issue to investigate this: https://github.com/coreos/coreos-assembler/issues/1489
		builder.Memory = int(math.Max(float64(builder.Memory), 10240))
	}
	return builder
}

type Install struct {
	CosaBuild       *sdk.LocalBuild
	Builder         *QemuBuilder
	Insecure        bool
	IgnitionSpec2   bool
	Native4k        bool
	PxeAppendRootfs bool

	// These are set by the install path
	kargs        []string
	ignition     conf.Conf
	liveIgnition conf.Conf
}

type InstalledMachine struct {
	Tempdir  string
	QemuInst *QemuInstance
}

func (inst *Install) PXE(kargs []string, liveIgnition, ignition conf.Conf, offline bool) (*InstalledMachine, error) {
	if inst.CosaBuild.Meta.BuildArtifacts.Metal == nil {
		return nil, fmt.Errorf("Build %s must have a `metal` artifact", inst.CosaBuild.Meta.OstreeVersion)
	}
	if inst.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		return nil, fmt.Errorf("build %s has no live installer kernel", inst.CosaBuild.Meta.Name)
	}

	inst.kargs = kargs
	inst.ignition = ignition
	inst.liveIgnition = liveIgnition

	mach, err := inst.runPXE(&kernelSetup{
		kernel:    inst.CosaBuild.Meta.BuildArtifacts.LiveKernel.Path,
		initramfs: inst.CosaBuild.Meta.BuildArtifacts.LiveInitramfs.Path,
		rootfs:    inst.CosaBuild.Meta.BuildArtifacts.LiveRootfs.Path,
	}, offline)
	if err != nil {
		return nil, errors.Wrapf(err, "testing live installer")
	}

	return mach, nil
}

func (inst *InstalledMachine) Destroy() error {
	if inst.QemuInst != nil {
		inst.QemuInst.Destroy()
		inst.QemuInst = nil
	}
	if inst.Tempdir != "" {
		return os.RemoveAll(inst.Tempdir)
	}
	return nil
}

type kernelSetup struct {
	kernel, initramfs, rootfs string
}

type pxeSetup struct {
	tftpipaddr    string
	boottype      string
	networkdevice string
	bootindex     string
	pxeimagepath  string

	// bootfile is initialized later
	bootfile string
}

type installerRun struct {
	inst    *Install
	builder *QemuBuilder

	builddir string
	tempdir  string
	tftpdir  string

	metalimg  string
	metalname string

	baseurl string

	kern kernelSetup
	pxe  pxeSetup
}

func absSymlink(src, dest string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return os.Symlink(src, dest)
}

// setupMetalImage creates a symlink to the metal image.
func setupMetalImage(builddir, metalimg, destdir string) (string, error) {
	if err := absSymlink(filepath.Join(builddir, metalimg), filepath.Join(destdir, metalimg)); err != nil {
		return "", err
	}
	return metalimg, nil
}

func (inst *Install) setup(kern *kernelSetup) (*installerRun, error) {
	if kern.kernel == "" {
		return nil, fmt.Errorf("Missing kernel artifact")
	}
	if kern.initramfs == "" {
		return nil, fmt.Errorf("Missing initramfs artifact")
	}
	if kern.rootfs == "" {
		return nil, fmt.Errorf("Missing rootfs artifact")
	}

	builder := inst.Builder

	tempdir, err := ioutil.TempDir("/var/tmp", "mantle-pxe")
	if err != nil {
		return nil, err
	}
	cleanupTempdir := true
	defer func() {
		if cleanupTempdir {
			os.RemoveAll(tempdir)
		}
	}()

	tftpdir := filepath.Join(tempdir, "tftp")
	if err := os.Mkdir(tftpdir, 0777); err != nil {
		return nil, err
	}

	builddir := inst.CosaBuild.Dir
	if err := inst.ignition.WriteFile(filepath.Join(tftpdir, "config.ign")); err != nil {
		return nil, err
	}
	// This code will ensure to add an SSH key to `pxe-live.ign` config.
	inst.liveIgnition.AddAutoLogin()
	if err := inst.liveIgnition.WriteFile(filepath.Join(tftpdir, "pxe-live.ign")); err != nil {
		return nil, err
	}

	for _, name := range []string{kern.kernel, kern.initramfs, kern.rootfs} {
		if err := absSymlink(filepath.Join(builddir, name), filepath.Join(tftpdir, name)); err != nil {
			return nil, err
		}
	}
	if inst.PxeAppendRootfs {
		// replace the initramfs symlink with a concatenation of
		// the initramfs and rootfs
		initrd := filepath.Join(tftpdir, kern.initramfs)
		if err := os.Remove(initrd); err != nil {
			return nil, err
		}
		if err := cat(initrd, filepath.Join(builddir, kern.initramfs), filepath.Join(builddir, kern.rootfs)); err != nil {
			return nil, err
		}
	}

	var metalimg string
	if inst.Native4k {
		metalimg = inst.CosaBuild.Meta.BuildArtifacts.Metal4KNative.Path
	} else {
		metalimg = inst.CosaBuild.Meta.BuildArtifacts.Metal.Path
	}
	metalname, err := setupMetalImage(builddir, metalimg, tftpdir)
	if err != nil {
		return nil, errors.Wrapf(err, "setting up metal image")
	}

	pxe := pxeSetup{}
	pxe.tftpipaddr = "192.168.76.2"
	switch system.RpmArch() {
	case "x86_64":
		pxe.boottype = "pxe"
		pxe.networkdevice = "e1000"
		pxe.pxeimagepath = "/usr/share/syslinux/"
		break
	case "aarch64":
		pxe.boottype = "grub"
		pxe.networkdevice = "virtio-net-pci"
		pxe.bootfile = "/boot/grub2/grubaa64.efi"
		pxe.pxeimagepath = "/boot/efi/EFI/fedora/grubaa64.efi"
		pxe.bootindex = "1"
		break
	case "ppc64le":
		pxe.boottype = "grub"
		pxe.networkdevice = "virtio-net-pci"
		pxe.bootfile = "/boot/grub2/powerpc-ieee1275/core.elf"
		break
	case "s390x":
		pxe.boottype = "pxe"
		pxe.networkdevice = "virtio-net-ccw"
		pxe.tftpipaddr = "10.0.2.2"
		pxe.bootindex = "1"
	default:
		return nil, fmt.Errorf("Unsupported arch %s" + system.RpmArch())
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(tftpdir)))
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// Yeah this leaks
	go func() {
		http.Serve(listener, mux)
	}()
	baseurl := fmt.Sprintf("http://%s:%d", pxe.tftpipaddr, port)

	cleanupTempdir = false // Transfer ownership
	return &installerRun{
		inst: inst,

		builder:  builder,
		tempdir:  tempdir,
		tftpdir:  tftpdir,
		builddir: builddir,

		metalimg:  metalimg,
		metalname: metalname,

		baseurl: baseurl,

		pxe:  pxe,
		kern: *kern,
	}, nil
}

func renderBaseKargs() []string {
	return append(baseKargs, fmt.Sprintf("console=%s", consoleKernelArgument[system.RpmArch()]))
}

func renderInstallKargs(t *installerRun, offline bool) []string {
	args := []string{"coreos.inst.install_dev=/dev/vda",
		fmt.Sprintf("coreos.inst.ignition_url=%s/config.ign", t.baseurl)}
	if !offline {
		args = append(args, fmt.Sprintf("coreos.inst.image_url=%s/%s", t.baseurl, t.metalname))
	}
	// FIXME - ship signatures by default too
	if t.inst.Insecure {
		args = append(args, "coreos.inst.insecure")
	}
	return args
}

func (t *installerRun) destroy() error {
	t.builder.Close()
	if t.tempdir != "" {
		return os.RemoveAll(t.tempdir)
	}
	return nil
}

func (t *installerRun) completePxeSetup(kargs []string) error {
	if t.kern.rootfs != "" && !t.inst.PxeAppendRootfs {
		kargs = append(kargs, fmt.Sprintf("coreos.live.rootfs_url=%s/%s", t.baseurl, t.kern.rootfs))
	}
	kargsStr := strings.Join(kargs, " ")

	switch t.pxe.boottype {
	case "pxe":
		pxeconfigdir := filepath.Join(t.tftpdir, "pxelinux.cfg")
		if err := os.Mkdir(pxeconfigdir, 0777); err != nil {
			return err
		}
		pxeimages := []string{"pxelinux.0", "ldlinux.c32"}
		pxeconfig := []byte(fmt.Sprintf(`
		DEFAULT pxeboot
		TIMEOUT 20
		PROMPT 0
		LABEL pxeboot
			KERNEL %s
			APPEND initrd=%s %s
		`, t.kern.kernel, t.kern.initramfs, kargsStr))
		if system.RpmArch() == "s390x" {
			pxeconfig = []byte(kargsStr)
		}
		ioutil.WriteFile(filepath.Join(pxeconfigdir, "default"), pxeconfig, 0777)

		// this is only for s390x where the pxe image has to be created;
		// s390 doesn't seem to have a pre-created pxe image although have to check on this
		if t.pxe.pxeimagepath == "" {
			kernelpath := filepath.Join(t.builddir, t.kern.kernel)
			initrdpath := filepath.Join(t.builddir, t.kern.initramfs)
			err := exec.Command("/usr/share/s390-tools/netboot/mk-s390image", kernelpath, "-r", initrdpath,
				"-p", filepath.Join(pxeconfigdir, "default"), filepath.Join(t.tftpdir, pxeimages[0])).Run()
			if err != nil {
				return err
			}
		} else {
			for _, img := range pxeimages {
				srcpath := filepath.Join("/usr/share/syslinux", img)
				if err := exec.Command("/usr/lib/coreos-assembler/cp-reflink", srcpath, t.tftpdir).Run(); err != nil {
					return err
				}
			}
		}
		t.pxe.bootfile = "/" + pxeimages[0]
		break
	case "grub":
		if err := exec.Command("grub2-mknetdir", "--net-directory="+t.tftpdir).Run(); err != nil {
			return err
		}
		if t.pxe.pxeimagepath != "" {
			dstpath := filepath.Join(t.tftpdir, "boot/grub2")
			if err := exec.Command("/usr/lib/coreos-assembler/cp-reflink", t.pxe.pxeimagepath, dstpath).Run(); err != nil {
				return err
			}
		}
		ioutil.WriteFile(filepath.Join(t.tftpdir, "boot/grub2/grub.cfg"), []byte(fmt.Sprintf(`
			default=0
			timeout=1
			menuentry "CoreOS (BIOS/UEFI)" {
				echo "Loading kernel"
				linux /%s %s
				echo "Loading initrd"
				initrd %s
			}
		`, t.kern.kernel, kargsStr, t.kern.initramfs)), 0777)
		break
	default:
		panic("Unhandled boottype " + t.pxe.boottype)
	}

	return nil
}

func cat(outfile string, infiles ...string) error {
	out, err := os.OpenFile(outfile, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, infile := range infiles {
		in, err := os.Open(infile)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(out, in)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *installerRun) run() (*QemuInstance, error) {
	builder := t.builder
	netdev := fmt.Sprintf("%s,netdev=mynet0,mac=52:54:00:12:34:56", t.pxe.networkdevice)
	if t.pxe.bootindex == "" {
		builder.Append("-boot", "once=n", "-option-rom", "/usr/share/qemu/pxe-rtl8139.rom")
	} else {
		netdev += fmt.Sprintf(",bootindex=%s", t.pxe.bootindex)
	}
	builder.Append("-device", netdev)
	usernetdev := fmt.Sprintf("user,id=mynet0,tftp=%s,bootfile=%s", t.tftpdir, t.pxe.bootfile)
	if t.pxe.tftpipaddr != "10.0.2.2" {
		usernetdev += ",net=192.168.76.0/24,dhcpstart=192.168.76.9"
	}
	builder.Append("-netdev", usernetdev)

	inst, err := builder.Exec()
	if err != nil {
		return nil, err
	}
	return inst, nil
}

func (inst *Install) runPXE(kern *kernelSetup, offline bool) (*InstalledMachine, error) {
	t, err := inst.setup(kern)
	if err != nil {
		return nil, err
	}
	defer t.destroy()

	kargs := renderBaseKargs()
	kargs = append(kargs, inst.kargs...)
	kargs = append(kargs, fmt.Sprintf("ignition.config.url=%s/pxe-live.ign", t.baseurl))

	kargs = append(kargs, renderInstallKargs(t, offline)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return nil, err
	}
	qinst, err := t.run()
	if err != nil {
		return nil, err
	}
	tempdir := t.tempdir
	t.tempdir = "" // Transfer ownership
	return &InstalledMachine{
		QemuInst: qinst,
		Tempdir:  tempdir,
	}, nil
}

func (inst *Install) InstallViaISOEmbed(kargs []string, liveIgnition, targetIgnition conf.Conf, offline bool) (*InstalledMachine, error) {
	if inst.CosaBuild.Meta.BuildArtifacts.Metal == nil {
		return nil, fmt.Errorf("Build %s must have a `metal` artifact", inst.CosaBuild.Meta.OstreeVersion)
	}
	if inst.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
		return nil, fmt.Errorf("Build %s must have a live ISO", inst.CosaBuild.Meta.Name)
	}

	if len(inst.kargs) > 0 {
		return nil, errors.New("injecting kargs is not supported yet, see https://github.com/coreos/coreos-installer/issues/164")
	}

	inst.kargs = kargs
	inst.ignition = targetIgnition
	inst.liveIgnition = liveIgnition

	tempdir, err := ioutil.TempDir("/var/tmp", "mantle-metal")
	if err != nil {
		return nil, err
	}
	cleanupTempdir := true
	defer func() {
		if cleanupTempdir {
			os.RemoveAll(tempdir)
		}
	}()

	if err := inst.ignition.WriteFile(filepath.Join(tempdir, "target.ign")); err != nil {
		return nil, err
	}

	builddir := inst.CosaBuild.Dir
	srcisopath := filepath.Join(builddir, inst.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	var metalimg string
	if inst.Native4k {
		metalimg = inst.CosaBuild.Meta.BuildArtifacts.Metal4KNative.Path
	} else {
		metalimg = inst.CosaBuild.Meta.BuildArtifacts.Metal.Path
	}
	metalname, err := setupMetalImage(builddir, metalimg, tempdir)
	if err != nil {
		return nil, errors.Wrapf(err, "setting up metal image")
	}

	var srcOpt string
	var serializedTargetConfig string
	if offline {
		// note we leave srcOpt as "" here; offline installs should now be the
		// default!

		// we want to test that a full offline install works; that includes the
		// final installed host booting offline
		serializedTargetConfig = inst.ignition.String()
	} else {
		mux := http.NewServeMux()
		mux.Handle("/", http.FileServer(http.Dir(tempdir)))
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		// Yeah this leaks
		go func() {
			http.Serve(listener, mux)
		}()
		baseurl := fmt.Sprintf("http://%s:%d", defaultQemuHostIPv4, port)
		srcOpt = fmt.Sprintf("--image-url %s/%s", baseurl, metalname)

		// In this case; the target config is jut a tiny wrapper that wants to
		// fetch our hosted target.ign config

		// TODO also use https://github.com/coreos/coreos-installer/issues/118#issuecomment-585572952
		// when it arrives
		targetConfig, err := conf.EmptyIgnition().Render(inst.IgnitionSpec2)
		if err != nil {
			return nil, err
		}
		targetConfig.AddConfigSource(baseurl + "/target.ign")
		serializedTargetConfig = targetConfig.String()
	}

	insecureOpt := ""
	if inst.Insecure {
		insecureOpt = "--insecure"
	}
	pointerIgnitionPath := "/var/opt/pointer.ign"

	targetDevice := "/dev/vda"
	// For aarch64, the cdrom is a pci blk device /dev/vda
	// TBD: use the serial identifier and use /dev/disk/by-id to install
	if system.RpmArch() == "aarch64" {
		targetDevice = "/dev/vdb"
	}

	installerUnit := fmt.Sprintf(`
[Unit]
After=network-online.target
Wants=network-online.target
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
RemainAfterExit=yes
Type=oneshot
ExecStart=/usr/bin/coreos-installer install %s --ignition %s %s %s
[Install]
WantedBy=multi-user.target
`, srcOpt, pointerIgnitionPath, insecureOpt, targetDevice)
	mode := 0644
	rebootUnitP := string(rebootUnit)

	inst.liveIgnition.AddSystemdUnit("coreos-installer.service", installerUnit, conf.Enable)
	inst.liveIgnition.AddSystemdUnit("coreos-installer-reboot.service", rebootUnitP, conf.Enable)
	inst.liveIgnition.AddFile(pointerIgnitionPath, "/", serializedTargetConfig, mode)
	inst.liveIgnition.AddAutoLogin()

	qemubuilder := inst.Builder
	qemubuilder.SetConfig(&inst.liveIgnition, inst.IgnitionSpec2)
	qemubuilder.AddIso(srcisopath, "bootindex=2")

	if offline {
		qemubuilder.Append("-nic", "none")
	}

	qinst, err := qemubuilder.Exec()
	if err != nil {
		return nil, err
	}
	cleanupTempdir = false // Transfer ownership
	return &InstalledMachine{
		QemuInst: qinst,
		Tempdir:  tempdir,
	}, nil
}
