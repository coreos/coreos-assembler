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

package qemu

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/platform"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/system/exec"
	"github.com/coreos/coreos-assembler/mantle/util"
)

const bootStartedSignal = "boot-started-OK"

// TODO derive this from docs, or perhaps include kargs in cosa metadata?
var baseKargs = []string{"rd.neednet=1", "ip=dhcp", "ignition.firstboot", "ignition.platform.id=metal"}

var (
	bootStartedUnit = fmt.Sprintf(`[Unit]
	Description=TestISO Boot Started
	Requires=dev-virtio\\x2dports-bootstarted.device
	OnFailure=emergency.target
	OnFailureJobMode=isolate
	[Service]
	Type=oneshot
	RemainAfterExit=yes
	ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/bootstarted'
	[Install]
	RequiredBy=coreos-installer.target
	`, bootStartedSignal)
)

// NewMetalQemuBuilderDefault returns a QEMU builder instance with some
// defaults set up for bare metal.
func NewMetalQemuBuilderDefault() *platform.QemuBuilder {
	builder := platform.NewQemuBuilder()
	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	builder.MemoryMiB = 4096
	return builder
}

type Install struct {
	CosaBuild       *util.LocalBuild
	Builder         *platform.QemuBuilder
	Insecure        bool
	Native4k        bool
	MultiPathDisk   bool
	PxeAppendRootfs bool
	NmKeyfiles      map[string]string

	// These are set by the install path
	kargs        []string
	ignition     conf.Conf
	liveIgnition conf.Conf
}

// Check that artifact has been built and locally exists
func (inst *Install) checkArtifactsExist(artifacts []string) error {
	version := inst.CosaBuild.Meta.OstreeVersion
	for _, name := range artifacts {
		artifact, err := inst.CosaBuild.Meta.GetArtifact(name)
		if err != nil {
			return fmt.Errorf("Missing artifact %s for %s build: %s", name, version, err)
		}
		path := filepath.Join(inst.CosaBuild.Dir, artifact.Path)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("Missing local file for artifact %s for build %s", name, version)
			}
		}
	}
	return nil
}

func (inst *Install) PXE(kargs []string, liveIgnition, ignition conf.Conf, offline bool) (*machine, error) {
	artifacts := []string{"live-kernel", "live-rootfs"}
	if err := inst.checkArtifactsExist(artifacts); err != nil {
		return nil, err
	}

	installerConfig := installerConfig{
		Console:     []string{platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]},
		AppendKargs: renderCosaTestIsoDebugKargs(),
	}
	installerConfigData, err := yaml.Marshal(installerConfig)
	if err != nil {
		return nil, err
	}
	mode := 0644

	// XXX: https://github.com/coreos/coreos-installer/issues/1171
	if coreosarch.CurrentRpmArch() != "s390x" {
		liveIgnition.AddFile("/etc/coreos/installer.d/mantle.yaml", string(installerConfigData), mode)
	}

	inst.kargs = append(renderCosaTestIsoDebugKargs(), kargs...)
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
	builder *platform.QemuBuilder

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
	var artifacts []string
	if inst.Native4k {
		artifacts = append(artifacts, "metal4k")
	} else {
		artifacts = append(artifacts, "metal")
	}
	if err := inst.checkArtifactsExist(artifacts); err != nil {
		return nil, err
	}

	builder := inst.Builder

	tempdir, err := os.MkdirTemp("/var/tmp", "mantle-pxe")
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
	inst.liveIgnition.AddSystemdUnit("boot-started.service", bootStartedUnit, conf.Enable)
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
	switch coreosarch.CurrentRpmArch() {
	case "x86_64":
		pxe.networkdevice = "e1000"
		if builder.Firmware == "uefi" {
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
		pxe.tftpipaddr = "10.0.2.2"
		pxe.bootindex = "1"
	default:
		return nil, fmt.Errorf("Unsupported arch %s", coreosarch.CurrentRpmArch())
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(tftpdir)))
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	//nolint // Yeah this leaks
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
	return append(baseKargs, fmt.Sprintf("console=%s", platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]))
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

// Sometimes the logs that stream from various virtio streams can be
// incomplete because they depend on services inside the guest.
// When you are debugging earlyboot/initramfs issues this can be
// problematic. Let's add a hook here to enable more debugging.
func renderCosaTestIsoDebugKargs() []string {
	if _, ok := os.LookupEnv("COSA_TESTISO_DEBUG"); ok {
		return []string{"systemd.log_color=0", "systemd.log_level=debug",
			"systemd.journald.forward_to_console=1",
			"systemd.journald.max_level_console=debug"}
	} else {
		return []string{}
	}
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
		`, t.kern.kernel, t.kern.initramfs, kargsStr))
		if coreosarch.CurrentRpmArch() == "s390x" {
			pxeconfig = []byte(kargsStr)
		}
		pxeconfig_path := filepath.Join(pxeconfigdir, "default")
		if err := os.WriteFile(pxeconfig_path, pxeconfig, 0777); err != nil {
			return errors.Wrapf(err, "writing file %s", pxeconfig_path)
		}

		// this is only for s390x where the pxe image has to be created;
		// s390 doesn't seem to have a pre-created pxe image although have to check on this
		if t.pxe.pxeimagepath == "" {
			kernelpath := filepath.Join(t.tftpdir, t.kern.kernel)
			initrdpath := filepath.Join(t.tftpdir, t.kern.initramfs)
			err := exec.Command("/usr/bin/mk-s390image", kernelpath, "-r", initrdpath,
				"-p", filepath.Join(pxeconfigdir, "default"), filepath.Join(t.tftpdir, pxeimages[0])).Run()
			if err != nil {
				return errors.Wrap(err, "running mk-s390image")
			}
		} else {
			for _, img := range pxeimages {
				srcpath := filepath.Join("/usr/share/syslinux", img)
				cp_cmd := exec.Command("/usr/lib/coreos-assembler/cp-reflink", srcpath, t.tftpdir)
				cp_cmd.Stderr = os.Stderr
				if err := cp_cmd.Run(); err != nil {
					return errors.Wrapf(err, "running cp-reflink %s %s", srcpath, t.tftpdir)
				}
			}
		}
		t.pxe.bootfile = "/" + pxeimages[0]
	case "grub":
		grub2_mknetdir_cmd := exec.Command("grub2-mknetdir", "--net-directory="+t.tftpdir)
		grub2_mknetdir_cmd.Stderr = os.Stderr
		if err := grub2_mknetdir_cmd.Run(); err != nil {
			return errors.Wrap(err, "running grub2-mknetdir")
		}
		if t.pxe.pxeimagepath != "" {
			dstpath := filepath.Join(t.tftpdir, "boot/grub2")
			cp_cmd := exec.Command("/usr/lib/coreos-assembler/cp-reflink", t.pxe.pxeimagepath, dstpath)
			cp_cmd.Stderr = os.Stderr
			if err := cp_cmd.Run(); err != nil {
				return errors.Wrapf(err, "running cp-reflink %s %s", t.pxe.pxeimagepath, dstpath)
			}
		}
		if err := os.WriteFile(filepath.Join(t.tftpdir, "boot/grub2/grub.cfg"), []byte(fmt.Sprintf(`
			default=0
			timeout=1
			menuentry "CoreOS (BIOS/UEFI)" {
				echo "Loading kernel"
				linux /%s %s
				echo "Loading initrd"
				initrd %s
			}
		`, t.kern.kernel, kargsStr, t.kern.initramfs)), 0777); err != nil {
			return errors.Wrap(err, "writing grub.cfg")
		}
	default:
		panic("Unhandled boottype " + t.pxe.boottype)
	}

	return nil
}

func switchBootOrderSignal(qinst *platform.QemuInstance, bootstartedchan *os.File, booterrchan *chan error) {
	*booterrchan = make(chan error)
	go func() {
		err := qinst.Wait()
		// only one Wait() gets process data, so also manually check for signal
		if err == nil && qinst.Signaled() {
			err = errors.New("process killed")
		}
		if err != nil {
			*booterrchan <- errors.Wrapf(err, "QEMU unexpectedly exited while waiting for %s", bootStartedSignal)
		}
	}()
	go func() {
		r := bufio.NewReader(bootstartedchan)
		l, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// this may be from QEMU getting killed or exiting; wait a bit
				// to give a chance for .Wait() above to feed the channel with a
				// better error
				time.Sleep(1 * time.Second)
				*booterrchan <- fmt.Errorf("Got EOF from boot started channel, %s expected", bootStartedSignal)
			} else {
				*booterrchan <- errors.Wrapf(err, "reading from boot started channel")
			}
			return
		}
		line := strings.TrimSpace(l)
		// switch the boot order here, we are well into the installation process - only for aarch64 and s390x
		if line == bootStartedSignal {
			if err := qinst.SwitchBootOrder(); err != nil {
				*booterrchan <- errors.Wrapf(err, "switching boot order failed")
				return
			}
		}
		// OK!
		*booterrchan <- nil
	}()
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

func (t *installerRun) run() (*platform.QemuInstance, error) {
	builder := t.builder
	netdev := fmt.Sprintf("%s,netdev=mynet0,mac=52:54:00:12:34:56", t.pxe.networkdevice)
	if t.pxe.bootindex == "" {
		builder.Append("-boot", "once=n")
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

func (inst *Install) runPXE(kern *kernelSetup, offline bool) (*machine, error) {
	t, err := inst.setup(kern)
	if err != nil {
		return nil, errors.Wrapf(err, "setting up install")
	}
	defer func() {
		err = t.destroy()
	}()

	bootStartedChan, err := inst.Builder.VirtioChannelRead("bootstarted")
	if err != nil {
		return nil, errors.Wrapf(err, "setting up bootstarted virtio-serial channel")
	}

	kargs := renderBaseKargs()
	kargs = append(kargs, inst.kargs...)
	kargs = append(kargs, fmt.Sprintf("ignition.config.url=%s/pxe-live.ign", t.baseurl))

	kargs = append(kargs, renderInstallKargs(t, offline)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return nil, errors.Wrapf(err, "completing PXE setup")
	}
	qinst, err := t.run()
	if err != nil {
		return nil, errors.Wrapf(err, "running PXE install")
	}
	tempdir := t.tempdir
	t.tempdir = "" // Transfer ownership
	instmachine := machine{
		inst:    qinst,
		tempdir: tempdir,
	}
	switchBootOrderSignal(qinst, bootStartedChan, &instmachine.bootStartedErrorChannel)
	return &instmachine, nil
}

// This object gets serialized to YAML and fed to coreos-installer:
// https://coreos.github.io/coreos-installer/customizing-install/#config-file-format
type installerConfig struct {
	ImageURL     string   `yaml:"image-url,omitempty"`
	IgnitionFile string   `yaml:"ignition-file,omitempty"`
	Insecure     bool     `yaml:",omitempty"`
	AppendKargs  []string `yaml:"append-karg,omitempty"`
	CopyNetwork  bool     `yaml:"copy-network,omitempty"`
	DestDevice   string   `yaml:"dest-device,omitempty"`
	Console      []string `yaml:"console,omitempty"`
}
