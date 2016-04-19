// Copyright 2015 CoreOS, Inc.
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
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/satori/go.uuid"
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform/local"
	"github.com/coreos/mantle/system/exec"
)

// QEMUOptions contains QEMU-specific options for the cluster.
type QEMUOptions struct {
	// DiskImage is the full path to the disk image to boot in QEMU.
	DiskImage string
	Board     string

	// BIOSImage is name of the BIOS file to pass to QEMU.
	// It can be a plain name, or a full path.
	BIOSImage string
}

// QEMUCluster is a local cluster of QEMU-based virtual machines.
//
// XXX: must be exported so that certain QEMU tests can access struct members
// through type assertions.
type QEMUCluster struct {
	*baseCluster
	conf QEMUOptions

	mu sync.Mutex
	*local.LocalCluster
}

type qemuMachine struct {
	qc          *QEMUCluster
	id          string
	qemu        exec.Cmd
	configDrive *local.ConfigDrive
	netif       *local.Interface
}

// NewQemuCluster creates a Cluster instance, suitable for running virtual
// machines in QEMU.
func NewQemuCluster(conf QEMUOptions) (Cluster, error) {
	lc, err := local.NewLocalCluster()
	if err != nil {
		return nil, err
	}

	bc, err := newBaseCluster()
	if err != nil {
		return nil, err
	}

	qc := &QEMUCluster{
		baseCluster:  bc,
		conf:         conf,
		LocalCluster: lc,
	}

	return Cluster(qc), nil
}

func (qc *QEMUCluster) Destroy() error {
	for _, qm := range qc.Machines() {
		qm.Destroy()
	}
	return qc.LocalCluster.Destroy()
}

func (qc *QEMUCluster) NewMachine(cfg string) (Machine, error) {
	id := uuid.NewV4()

	// hacky solution for cloud config ip substitution
	// NOTE: escaping is not supported
	qc.mu.Lock()
	netif := qc.Dnsmasq.GetInterface("br0")
	ip := strings.Split(netif.DHCPv4[0].String(), "/")[0]

	cfg = strings.Replace(cfg, "$public_ipv4", ip, -1)
	cfg = strings.Replace(cfg, "$private_ipv4", ip, -1)

	conf, err := NewConf(cfg)
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}

	keys, err := qc.SSHAgent.List()
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

	qm := &qemuMachine{
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

	cmd := qm.qemu.(*local.NsCmd)
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = append(cmd.ExtraFiles, tap.File) // fd=3

	if err = qm.qemu.Start(); err != nil {
		return nil, err
	}

	if err := commonMachineChecks(qm); err != nil {
		qm.Destroy()
		return nil, err
	}

	qc.addMach(qm)

	return Machine(qm), nil
}

// overrides baseCluster.GetDiscoveryURL
func (qc *QEMUCluster) GetDiscoveryURL(size int) (string, error) {
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

func (m *qemuMachine) ID() string {
	return m.id
}

func (m *qemuMachine) IP() string {
	return m.netif.DHCPv4[0].IP.String()
}

func (m *qemuMachine) PrivateIP() string {
	return m.netif.DHCPv4[0].IP.String()
}

func (m *qemuMachine) SSHClient() (*ssh.Client, error) {
	sshClient, err := m.qc.SSHAgent.NewClient(m.IP())
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (m *qemuMachine) SSH(cmd string) ([]byte, error) {
	client, err := m.SSHClient()
	if err != nil {
		return nil, err
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	defer session.Close()

	session.Stderr = os.Stderr
	out, err := session.Output(cmd)
	out = bytes.TrimSpace(out)
	return out, err
}

func (m *qemuMachine) Destroy() error {
	err := m.qemu.Kill()

	if m.configDrive != nil {
		err2 := m.configDrive.Destroy()
		if err == nil && err2 != nil {
			err = err2
		}
	}

	m.qc.delMach(m)

	return err
}
