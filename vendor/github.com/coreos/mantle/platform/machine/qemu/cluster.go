// Copyright 2016 CoreOS, Inc.
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
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/satori/go.uuid"

	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/local"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/ns"
)

// Options contains QEMU-specific options for the cluster.
type Options struct {
	// DiskImage is the full path to the disk image to boot in QEMU.
	DiskImage string
	Board     string

	// BIOSImage is name of the BIOS file to pass to QEMU.
	// It can be a plain name, or a full path.
	BIOSImage string

	*platform.Options
}

// Cluster is a local cluster of QEMU-based virtual machines.
//
// XXX: must be exported so that certain QEMU tests can access struct members
// through type assertions.
type Cluster struct {
	*platform.BaseCluster
	conf *Options

	mu sync.Mutex
	*local.LocalCluster
}

// NewCluster creates a Cluster instance, suitable for running virtual
// machines in QEMU.
func NewCluster(conf *Options) (platform.Cluster, error) {
	lc, err := local.NewLocalCluster()
	if err != nil {
		return nil, err
	}

	nsdialer := network.NewNsDialer(lc.GetNsHandle())
	bc, err := platform.NewBaseClusterWithDialer(conf.BaseName, nsdialer)
	if err != nil {
		return nil, err
	}

	qc := &Cluster{
		BaseCluster:  bc,
		conf:         conf,
		LocalCluster: lc,
	}

	return qc, nil
}

func (qc *Cluster) Destroy() error {
	if err := qc.BaseCluster.Destroy(); err != nil {
		return err
	}

	return qc.LocalCluster.Destroy()
}

func (qc *Cluster) NewMachine(cfg string) (platform.Machine, error) {
	id := uuid.NewV4()

	// hacky solution for cloud config ip substitution
	// NOTE: escaping is not supported
	qc.mu.Lock()
	netif := qc.Dnsmasq.GetInterface("br0")
	ip := strings.Split(netif.DHCPv4[0].String(), "/")[0]

	cfg = strings.Replace(cfg, "$public_ipv4", ip, -1)
	cfg = strings.Replace(cfg, "$private_ipv4", ip, -1)

	conf, err := conf.New(cfg)
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}

	keys, err := qc.Keys()
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}

	conf.CopyKeys(keys)

	qc.mu.Unlock()

	configDrive, err := local.NewConfigDrive(conf.String())
	if err != nil {
		return nil, err
	}

	qm := &machine{
		qc:          qc,
		id:          id.String(),
		configDrive: configDrive,
		netif:       netif,
	}

	imageFile, err := setupDisk(qc.conf.DiskImage)
	if err != nil {
		return nil, err
	}
	defer os.Remove(imageFile)

	qc.mu.Lock()

	tap, err := qc.NewTap("br0")
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}
	defer tap.Close()

	qmMac := qm.netif.HardwareAddr.String()
	qmCfg := qm.configDrive.Directory
	if qc.conf.Board == "arm64-usr" {
		qm.qemu = qm.qc.NewCommand(
			"qemu-system-aarch64",
			"-machine", "virt",
			"-cpu", "cortex-a57",
			"-bios", qc.conf.BIOSImage,
			"-smp", "1",
			"-m", "1024",
			"-uuid", qm.id,
			"-display", "none",
			"-drive", "if=none,id=blk,format=raw,file="+imageFile,
			"-device", "virtio-blk-device,drive=blk",
			"-netdev", "tap,id=tap,fd=3",
			"-device", "virtio-net-device,netdev=tap,mac="+qmMac,
			"-fsdev", "local,id=cfg,security_model=none,readonly,path="+qmCfg,
			"-device", "virtio-9p-device,fsdev=cfg,mount_tag=config-2")
	} else {
		qm.qemu = qm.qc.NewCommand(
			"qemu-system-x86_64",
			"-machine", "accel=kvm",
			"-cpu", "host",
			"-bios", qc.conf.BIOSImage,
			"-smp", "1",
			"-m", "1024",
			"-uuid", qm.id,
			"-display", "none",
			"-drive", "if=none,id=blk,format=raw,file="+imageFile,
			"-device", "virtio-blk-pci,drive=blk",
			"-netdev", "tap,id=tap,fd=3",
			"-device", "virtio-net,netdev=tap,mac="+qmMac,
			"-fsdev", "local,id=cfg,security_model=none,readonly,path="+qmCfg,
			"-device", "virtio-9p-pci,fsdev=cfg,mount_tag=config-2")
	}

	qc.mu.Unlock()

	cmd := qm.qemu.(*ns.Cmd)
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = append(cmd.ExtraFiles, tap.File) // fd=3

	if err = qm.qemu.Start(); err != nil {
		return nil, err
	}

	if err := platform.CheckMachine(qm); err != nil {
		qm.Destroy()
		return nil, err
	}

	if err := platform.EnableSelinux(qm); err != nil {
		qm.Destroy()
		return nil, err
	}
	qc.AddMach(qm)

	return qm, nil
}

// overrides BaseCluster.GetDiscoveryURL
func (qc *Cluster) GetDiscoveryURL(size int) (string, error) {
	return qc.LocalCluster.GetDiscoveryURL(size)
}

// Copy the base image to a new nameless temporary file.
// cp is used since it supports sparse and reflink.
func setupDisk(imageFile string) (string, error) {
	dstFile, err := ioutil.TempFile("", "mantle-qemu")
	if err != nil {
		return "", err
	}
	dstFileName := dstFile.Name()
	dstFile.Close()

	cp := exec.Command("cp", "--force",
		"--sparse=always", "--reflink=auto",
		imageFile, dstFileName)
	cp.Stdout = os.Stdout
	cp.Stderr = os.Stderr

	if err := cp.Run(); err != nil {
		return "", err
	}

	return dstFileName, nil
}
