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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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
		Run:    runTestIso,
		PreRun: preRun,
		Use:    "testiso",
		Short:  "Test a CoreOS PXE boot or ISO install path",
	}
	// TODO expose this as an API that can be used by cosa too
	consoleKernelArgument = map[string]string{
		"amd64-usr":   "ttyS0",
		"ppc64le-usr": "hvc0",
		"arm64-usr":   "ttyAMA0",
		"s390x-usr":   "ttysclp0",
	}
)

func init() {
	root.AddCommand(cmdTestIso)
}

func runTestIso(cmd *cobra.Command, args []string) {
	if err := doTestIso(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func doTestIso(cmd *cobra.Command, args []string) error {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --cosa-build")
	}

	if kola.CosaBuild.BuildArtifacts.Kernel.Path != "" {
		if err := testLegacyInstaller(); err != nil {
			return errors.Wrapf(err, "testing legacy installer")
		}
		return nil
	} else {
		return fmt.Errorf("build %s has no installer kernel", kola.CosaBuild.Name)
	}
}

func testLegacyInstaller() error {
	builddir := filepath.Dir(kola.Options.CosaBuild)
	metalimg := kola.CosaBuild.BuildArtifacts.Metal.Path

	builder := platform.NewBuilder(kola.QEMUOptions.Board, "")
	defer builder.Close()
	builder.Firmware = kola.QEMUOptions.Firmware
	builder.AddDisk(&platform.Disk{
		Size: "12G", // Arbitrary
	})
	// Arbitrary
	builder.Memory = 1536

	builder.InheritConsole = true

	tftpipaddr := "192.168.76.2"
	var boottype, networkdevice, bootindex, pxeimagepath string
	switch kola.QEMUOptions.Board {
	case "amd64-usr":
		boottype = "pxe"
		networkdevice = "e1000"
		pxeimagepath = "/usr/share/syslinux/"
		break
	case "ppc64le-usr":
		boottype = "grub"
		networkdevice = "virtio-net-pci"
		break
	case "s390x-usr":
		boottype = "pxe"
		networkdevice = "virtio-net-ccw"
		tftpipaddr = "10.0.2.2"
		bootindex = "1"
		builder.Memory = 16384
	default:
		return fmt.Errorf("Unsupported arch %s", kola.QEMUOptions.Board)
	}

	unit := `[Unit]
	Requires=dev-virtio\\x2dports-completion.device
	OnFailure=emergency.target
	OnFailureJobMode=isolate
	[Service]
	Type=oneshot
	ExecStart=/bin/sh -c '/usr/bin/echo coreos-installer-test-OK >/dev/virtio-ports/completion && systemctl poweroff'
	[Install]
	RequiredBy=multi-user.target
`
	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "coreos-test-installer.service",
					Contents: &unit,
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

	tempdir, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}

	tftpdir := filepath.Join(tempdir, "tftp")
	if err := os.Mkdir(tftpdir, 0777); err != nil {
		return err
	}
	defer os.RemoveAll(tftpdir)

	if err := ioutil.WriteFile(filepath.Join(tftpdir, "config.ign"), serializedConfig, 0777); err != nil {
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
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(tftpdir)))
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	go func() {
		http.Serve(listener, mux)
	}()
	baseurl := fmt.Sprintf("http://%s:%d", tftpipaddr, port)

	for _, name := range []string{kola.CosaBuild.BuildArtifacts.Kernel.Path, kola.CosaBuild.BuildArtifacts.Initramfs.Path} {
		src, err := filepath.Abs(filepath.Join(builddir, name))
		if err != nil {
			return err
		}
		if err := os.Symlink(src, filepath.Join(tftpdir, name)); err != nil {
			return err
		}
	}

	kargs := []string{"rd.neednet=1", "ip=dhcp", "coreos.inst=yes", "coreos.inst.install_dev=vda"}
	kargs = append(kargs, fmt.Sprintf("console=%s", consoleKernelArgument[kola.QEMUOptions.Board]))
	kargs = append(kargs, fmt.Sprintf("coreos.inst.image_url=%s/%s", baseurl, metalname))
	kargs = append(kargs, fmt.Sprintf("coreos.inst.ignition_url=%s/config.ign", baseurl))

	kargsStr := strings.Join(kargs, " ")

	var bootfile string
	switch boottype {
	case "pxe":
		pxeconfigdir := filepath.Join(tftpdir, "pxelinux.cfg")
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
		`, kola.CosaBuild.BuildArtifacts.Kernel.Path, kola.CosaBuild.BuildArtifacts.Initramfs.Path, kargsStr))
		if kola.QEMUOptions.Board == "s390x-usr" {
			pxeconfig = []byte(kargsStr)
		}
		ioutil.WriteFile(filepath.Join(pxeconfigdir, "default"), pxeconfig, 0777)

		// this is only for s390x where the pxe image has to be created;
		// s390 doesn't seem to have a pre-created pxe image although have to check on this
		if pxeimagepath == "" {
			kernelpath := filepath.Join(builddir, kola.CosaBuild.BuildArtifacts.Kernel.Path)
			initrdpath := filepath.Join(builddir, kola.CosaBuild.BuildArtifacts.Initramfs.Path)
			err := exec.Command("/usr/share/s390-tools/netboot/mk-s390image", kernelpath, "-r", initrdpath,
				"-p", filepath.Join(pxeconfigdir, "default"), filepath.Join(tftpdir, pxeimages[0])).Run()
			if err != nil {
				return err
			}
		} else {
			for _, img := range pxeimages {
				srcpath := filepath.Join("/usr/share/syslinux", img)
				if err := exec.Command("/usr/lib/coreos-assembler/cp-reflink", srcpath, tftpdir).Run(); err != nil {
					return err
				}
			}
		}
		bootfile = "/" + pxeimages[0]
		break
	case "grub":
		bootfile = "/boot/grub2/powerpc-ieee1275/core.elf"
		if err := exec.Command("grub2-mknetdir", "--net-directory="+tftpdir).Run(); err != nil {
			return err
		}
		ioutil.WriteFile(filepath.Join(tftpdir, "boot/grub2/grub.cfg"), []byte(fmt.Sprintf(`
			default=0
			timeout=1
			menuentry "CoreOS (BIOS)" {
				echo "Loading kernel"
				linux /%s %s
				echo "Loading initrd"
				initrd %s
			}
		`, kola.CosaBuild.BuildArtifacts.Kernel.Path, kargsStr, kola.CosaBuild.BuildArtifacts.Initramfs.Path)), 0777)
		break
	default:
		panic("Unhandled boottype " + boottype)
	}

	completionfile := filepath.Join(tempdir, "completion.txt")
	completionstamp := "coreos-installer-test-OK"

	netdev := fmt.Sprintf("%s,netdev=mynet0,mac=52:54:00:12:34:56", networkdevice)
	if bootindex == "" {
		builder.Append("-boot", "once=n", "-option-rom", "/usr/share/qemu/pxe-rtl8139.rom")
	} else {
		netdev += fmt.Sprintf(",bootindex=%s", bootindex)
	}
	builder.Append("-device", netdev)
	usernetdev := fmt.Sprintf("user,id=mynet0,tftp=%s,bootfile=%s", tftpdir, bootfile)
	if tftpipaddr != "10.0.2.2" {
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

	fmt.Printf("Successfully tested legacy installer for %s\n", kola.CosaBuild.OstreeVersion)

	return nil
}
