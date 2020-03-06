// Copyright 2020 Red Hat, Inc.
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

// TODO:
// - Support testing the "just run Live" case - maybe try to figure out
//   how to have main `kola` tests apply?
// - Test `coreos-install iso embed` path

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/sdk"

	ignconverter "github.com/coreos/ign-converter"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
)

var (
	cmdTestIso = &cobra.Command{
		RunE:    runTestIso,
		PreRunE: preRun,
		Use:     "testiso",
		Short:   "Test a CoreOS PXE boot or ISO install path",

		SilenceUsage: true,
	}
	// TODO expose this as an API that can be used by cosa too
	consoleKernelArgument = map[string]string{
		"amd64-usr":   "ttyS0",
		"ppc64le-usr": "hvc0",
		"arm64-usr":   "ttyAMA0",
		"s390x-usr":   "ttysclp0",
	}

	instInsecure bool

	legacy bool
	nolive bool
)

func init() {
	cmdTestIso.Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	cmdTestIso.Flags().BoolVarP(&legacy, "legacy", "K", false, "Test legacy installer")
	cmdTestIso.Flags().BoolVarP(&nolive, "no-live", "L", false, "Skip testing live installer")

	root.AddCommand(cmdTestIso)
}

type kernelSetup struct {
	kernel, initramfs string
}

func runTestIso(cmd *cobra.Command, args []string) error {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --cosa-build")
	}

	if kola.CosaBuild.BuildArtifacts.Metal.Path == "" {
		return fmt.Errorf("Build %s must have a `metal` artifact", kola.CosaBuild.OstreeVersion)
	}

	ranTest := false

	foundLegacy := kola.CosaBuild.BuildArtifacts.Kernel.Path != ""
	if foundLegacy {
		if legacy {
			ranTest = true
			if err := testLegacyInstaller(&kernelSetup{
				kernel:    kola.CosaBuild.BuildArtifacts.Kernel.Path,
				initramfs: kola.CosaBuild.BuildArtifacts.Initramfs.Path,
			}); err != nil {
				return errors.Wrapf(err, "testing legacy installer")
			}
		}
	} else if legacy {
		return fmt.Errorf("build %s has no legacy installer kernel", kola.CosaBuild.Name)
	}

	foundLive := kola.CosaBuild.BuildArtifacts.LiveKernel.Path != ""
	if !nolive {
		if !foundLive {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Name)
		}
		ranTest = true
		if err := testLiveInstaller(&kernelSetup{
			kernel:    kola.CosaBuild.BuildArtifacts.LiveKernel.Path,
			initramfs: kola.CosaBuild.BuildArtifacts.LiveInitramfs.Path,
		}); err != nil {
			return errors.Wrapf(err, "testing live installer")
		}
	}

	if !ranTest {
		return fmt.Errorf("Nothing to test!")
	}

	return nil
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

type installerTest struct {
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

var signalCompletionUnit = `[Unit]
Requires=dev-virtio\\x2dports-completion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
ExecStart=/bin/sh -c '/usr/bin/echo coreos-installer-test-OK >/dev/virtio-ports/completion && systemctl poweroff'
[Install]
RequiredBy=multi-user.target
`

// TODO derive this from docs, or perhaps include kargs in cosa metadata?
var baseKargs = []string{"rd.neednet=1", "ip=dhcp"}
var liveKargs = []string{"ignition.firstboot", "ignition.platform.id=metal"}

func absSymlink(src, dest string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return os.Symlink(src, dest)
}

func setupTftpDir(builddir, tftpdir, metalimg string, kern *kernelSetup) error {
	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "coreos-test-installer.service",
					Contents: &signalCompletionUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
	var serializedConfig []byte
	if sdk.TargetIgnitionVersionFromName(metalimg) == "v2" {
		ignc2, err := ignconverter.Translate3to2(config)
		if err != nil {
			return err
		}
		buf, err := json.Marshal(ignc2)
		if err != nil {
			return err
		}
		serializedConfig = buf
	} else {
		buf, err := json.Marshal(config)
		if err != nil {
			return err
		}
		serializedConfig = buf
	}

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

func setupTest(kern *kernelSetup) (*installerTest, error) {
	if kern.kernel == "" {
		return nil, fmt.Errorf("Missing kernel artifact")
	}
	if kern.initramfs == "" {
		return nil, fmt.Errorf("Missing initramfs artifact")
	}

	builder := platform.NewBuilder(kola.QEMUOptions.Board, "")
	builder.Firmware = kola.QEMUOptions.Firmware
	builder.AddDisk(&platform.Disk{
		Size: "12G", // Arbitrary
	})

	// This applies just in the legacy case
	builder.Memory = 1542
	if kola.QEMUOptions.Board == "s390x-usr" {
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

	builddir := filepath.Dir(kola.Options.CosaBuild)
	metalimg := kola.CosaBuild.BuildArtifacts.Metal.Path
	metalname := metalimg
	// Yeah this is duplicated with setupTftpDir
	if strings.HasSuffix(metalimg, ".raw") {
		metalname = metalimg + ".gz"
	}

	if err := setupTftpDir(builddir, tftpdir, metalimg, kern); err != nil {
		return nil, err
	}

	pxe := pxeSetup{}
	pxe.tftpipaddr = "192.168.76.2"
	switch kola.QEMUOptions.Board {
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
		return nil, fmt.Errorf("Unsupported arch %s", kola.QEMUOptions.Board)
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
	return &installerTest{
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

func renderBaseKargs(t *installerTest) []string {
	return append(baseKargs, fmt.Sprintf("console=%s", consoleKernelArgument[kola.QEMUOptions.Board]))
}

func renderInstallKargs(t *installerTest) []string {
	args := []string{"coreos.inst=yes", "coreos.inst.install_dev=vda",
		fmt.Sprintf("coreos.inst.image_url=%s/%s", t.baseurl, t.metalname),
		fmt.Sprintf("coreos.inst.ignition_url=%s/config.ign", t.baseurl)}
	// FIXME - ship signatures by default too
	if instInsecure {
		args = append(args, "coreos.inst.insecure=1")
	}
	return args
}

func (t *installerTest) destroy() error {
	t.builder.Close()
	return os.RemoveAll(t.tempdir)
}

func (t *installerTest) completePxeSetup(kargs []string) error {
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
		if kola.QEMUOptions.Board == "s390x-usr" {
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

func (t *installerTest) run() error {
	completionfile := filepath.Join(t.tempdir, "completion.txt")
	completionstamp := "coreos-installer-test-OK"

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
	builder.Append("-device", "virtio-serial", "-device", "virtserialport,chardev=completion,name=completion")
	builder.Append("-chardev", "file,id=completion,path="+completionfile)

	inst, err := builder.Exec()
	if err != nil {
		return err
	}

	err = inst.Wait()
	if err != nil {
		return err
	}

	err = exec.Command("grep", "-q", "-e", completionstamp, completionfile).Run()
	if err != nil {
		return fmt.Errorf("Failed to find %s in %s: %s", completionstamp, completionfile, err)
	}

	return nil
}

func testLegacyInstaller(kern *kernelSetup) error {
	t, err := setupTest(kern)
	if err != nil {
		return err
	}
	defer t.destroy()

	kargs := append(renderBaseKargs(t), renderInstallKargs(t)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return err
	}
	if err := t.run(); err != nil {
		return err
	}

	fmt.Printf("Successfully tested legacy installer for %s\n", kola.CosaBuild.OstreeVersion)

	return nil
}

func testLiveInstaller(kern *kernelSetup) error {
	t, err := setupTest(kern)
	if err != nil {
		return err
	}
	defer t.destroy()

	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	t.builder.Memory = int(math.Max(float64(t.builder.Memory), 4096))

	kargs := append(renderBaseKargs(t), liveKargs...)
	kargs = append(kargs, renderInstallKargs(t)...)
	if err := t.completePxeSetup(kargs); err != nil {
		return err
	}
	if err := t.run(); err != nil {
		return err
	}

	fmt.Printf("Successfully tested live installer for %s\n", kola.CosaBuild.OstreeVersion)

	return nil
}
