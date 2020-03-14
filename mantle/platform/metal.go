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
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/cosa"
	"github.com/coreos/mantle/system/exec"
	"github.com/pkg/errors"
)

// TODO derive this from docs, or perhaps include kargs in cosa metadata?
var baseKargs = []string{"rd.neednet=1", "ip=dhcp"}
var liveKargs = []string{"ignition.firstboot", "ignition.platform.id=metal"}

var (
	// TODO expose this as an API that can be used by cosa too
	consoleKernelArgument = map[string]string{
		"amd64-usr":   "ttyS0",
		"ppc64le-usr": "hvc0",
		"arm64-usr":   "ttyAMA0",
		"s390x-usr":   "ttysclp0",
	}
)

type Install struct {
	CosaBuildDir string
	CosaBuild    *cosa.Build

	Board    string
	Firmware string
	Insecure bool
	QemuArgs []string

	LegacyInstaller bool

	// These are set by the install path
	kargs    []string
	ignition string
}

type InstalledMachine struct {
	tempdir  string
	QemuInst *QemuInstance
}

func (inst *Install) PXE(kargs []string, ignition string) (*InstalledMachine, error) {
	if inst.CosaBuild.BuildArtifacts.Metal == nil {
		return nil, fmt.Errorf("Build %s must have a `metal` artifact", inst.CosaBuild.OstreeVersion)
	}

	inst.kargs = kargs
	inst.ignition = ignition

	var err error
	var mach *InstalledMachine
	if inst.LegacyInstaller {
		if inst.CosaBuild.BuildArtifacts.Kernel == nil {
			return nil, fmt.Errorf("build %s has no legacy installer kernel", inst.CosaBuild.OstreeVersion)
		}
		mach, err = inst.runLegacy(&kernelSetup{
			kernel:    inst.CosaBuild.BuildArtifacts.Kernel.Path,
			initramfs: inst.CosaBuild.BuildArtifacts.Initramfs.Path,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "legacy installer")
		}
	} else {
		if inst.CosaBuild.BuildArtifacts.LiveKernel == nil {
			return nil, fmt.Errorf("build %s has no live installer kernel", inst.CosaBuild.Name)
		}
		mach, err = inst.runLive(&kernelSetup{
			kernel:    inst.CosaBuild.BuildArtifacts.LiveKernel.Path,
			initramfs: inst.CosaBuild.BuildArtifacts.LiveInitramfs.Path,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "testing live installer")
		}
	}

	return mach, nil
}

func (inst *InstalledMachine) Destroy() error {
	if inst.tempdir != "" {
		return os.RemoveAll(inst.tempdir)
	}
	return nil
}

type kernelSetup struct {
	kernel, initramfs string
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

func (inst *Install) setupTftpDir(builddir, tftpdir, metalimg string, kern *kernelSetup) error {
	serializedConfig := []byte(inst.ignition)
	if err := ioutil.WriteFile(filepath.Join(tftpdir, "config.ign"), serializedConfig, 0644); err != nil {
		return err
	}

	metalIsCompressed := !strings.HasSuffix(metalimg, ".raw")
	metalname := metalimg
	if !metalIsCompressed {
		fmt.Println("Compressing metal image")
		metalimgpath := filepath.Join(builddir, metalimg)
		srcf, err := os.Open(metalimgpath)
		if err != nil {
			return err
		}
		defer srcf.Close()
		metalname = metalname + ".gz"
		destf, err := os.OpenFile(filepath.Join(tftpdir, metalname), os.O_RDWR|os.O_CREATE, 0755)
		if err != nil {
			return err
		}
		defer destf.Close()
		cmd := exec.Command("gzip", "-1")
		cmd.Stdin = srcf
		cmd.Stdout = destf
		if err := cmd.Run(); err != nil {
			return errors.Wrapf(err, "running gzip")
		}
	} else {
		if err := absSymlink(filepath.Join(builddir, metalimg), filepath.Join(tftpdir, metalimg)); err != nil {
			return err
		}
	}

	for _, name := range []string{kern.kernel, kern.initramfs} {
		if err := absSymlink(filepath.Join(builddir, name), filepath.Join(tftpdir, name)); err != nil {
			return err
		}
	}

	return nil
}

func (inst *Install) setup(kern *kernelSetup) (*installerRun, error) {
	if kern.kernel == "" {
		return nil, fmt.Errorf("Missing kernel artifact")
	}
	if kern.initramfs == "" {
		return nil, fmt.Errorf("Missing initramfs artifact")
	}

	builder := NewBuilder(inst.Board, "", false)
	builder.Firmware = inst.Firmware
	builder.AddDisk(&Disk{
		Size: "12G", // Arbitrary
	})

	// This applies just in the legacy case
	builder.Memory = 1536
	if inst.Board == "s390x-usr" {
		// FIXME - determine why this is
		builder.Memory = int(math.Max(float64(builder.Memory), 16384))
	}

	// For now, but in the future we should rely on log capture
	builder.InheritConsole = true

	tempdir, err := ioutil.TempDir("", "kola-testiso")
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

	builddir := filepath.Dir(inst.CosaBuildDir)
	metalimg := inst.CosaBuild.BuildArtifacts.Metal.Path
	metalname := metalimg
	// Yeah this is duplicated with setupTftpDir
	if strings.HasSuffix(metalimg, ".raw") {
		metalname = metalimg + ".gz"
	}

	if err := inst.setupTftpDir(builddir, tftpdir, metalimg, kern); err != nil {
		return nil, err
	}

	pxe := pxeSetup{}
	pxe.tftpipaddr = "192.168.76.2"
	switch inst.Board {
	case "amd64-usr":
		pxe.boottype = "pxe"
		pxe.networkdevice = "e1000"
		pxe.pxeimagepath = "/usr/share/syslinux/"
		break
	case "ppc64le-usr":
		pxe.boottype = "grub"
		pxe.networkdevice = "virtio-net-pci"
		break
	case "s390x-usr":
		pxe.boottype = "pxe"
		pxe.networkdevice = "virtio-net-ccw"
		pxe.tftpipaddr = "10.0.2.2"
		pxe.bootindex = "1"
	default:
		return nil, fmt.Errorf("Unsupported arch %s", inst.Board)
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

func renderBaseKargs(t *installerRun) []string {
	return append(baseKargs, fmt.Sprintf("console=%s", consoleKernelArgument[t.inst.Board]))
}

func renderInstallKargs(t *installerRun) []string {
	args := []string{"coreos.inst=yes", "coreos.inst.install_dev=vda",
		fmt.Sprintf("coreos.inst.image_url=%s/%s", t.baseurl, t.metalname),
		fmt.Sprintf("coreos.inst.ignition_url=%s/config.ign", t.baseurl)}
	// FIXME - ship signatures by default too
	if t.inst.Insecure {
		args = append(args, "coreos.inst.insecure=1")
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
	kargsStr := strings.Join(kargs, " ")

	var bootfile string
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
		if t.inst.Board == "s390x-usr" {
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
		bootfile = "/" + pxeimages[0]
		break
	case "grub":
		bootfile = "/boot/grub2/powerpc-ieee1275/core.elf"
		if err := exec.Command("grub2-mknetdir", "--net-directory="+t.tftpdir).Run(); err != nil {
			return err
		}
		ioutil.WriteFile(filepath.Join(t.tftpdir, "boot/grub2/grub.cfg"), []byte(fmt.Sprintf(`
			default=0
			timeout=1
			menuentry "CoreOS (BIOS)" {
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

	t.pxe.bootfile = bootfile

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
	builder.Append(t.inst.QemuArgs...)

	inst, err := builder.Exec()
	if err != nil {
		return nil, err
	}
	return inst, nil
}

func (inst *Install) runLegacy(kern *kernelSetup) (*InstalledMachine, error) {
	t, err := inst.setup(kern)
	if err != nil {
		return nil, err
	}
	defer t.destroy()

	kargs := append(renderBaseKargs(t), renderInstallKargs(t)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return nil, err
	}
	qinst, err := t.run()
	if err != nil {
		return nil, err
	}
	t.tempdir = "" // Transfer ownership
	return &InstalledMachine{
		QemuInst: qinst,
		tempdir:  t.tempdir,
	}, nil
}

func (inst *Install) runLive(kern *kernelSetup) (*InstalledMachine, error) {
	t, err := inst.setup(kern)
	if err != nil {
		return nil, err
	}
	defer t.destroy()

	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	t.builder.Memory = int(math.Max(float64(t.builder.Memory), 4096))

	kargs := append(renderBaseKargs(t), liveKargs...)
	kargs = append(kargs, renderInstallKargs(t)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return nil, err
	}
	qinst, err := t.run()
	if err != nil {
		return nil, err
	}
	t.tempdir = "" // Transfer ownership
	return &InstalledMachine{
		QemuInst: qinst,
		tempdir:  t.tempdir,
	}, nil
}
