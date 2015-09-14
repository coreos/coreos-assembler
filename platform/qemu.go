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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/satori/go.uuid"
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/coreos/mantle/platform/local"
	"github.com/coreos/mantle/util"
)

type QEMUOptions struct {
	DiskImage string
}

type qemuCluster struct {
	*local.LocalCluster
	machines map[string]*qemuMachine
	conf     QEMUOptions
}

type qemuMachine struct {
	qc          *qemuCluster
	id          string
	qemu        util.Cmd
	configDrive *local.ConfigDrive
	netif       *local.Interface
	sshClient   *ssh.Client
}

func NewQemuCluster(conf QEMUOptions) (Cluster, error) {
	lc, err := local.NewLocalCluster()
	if err != nil {
		return nil, err
	}

	qc := &qemuCluster{
		LocalCluster: lc,
		machines:     make(map[string]*qemuMachine),
		conf:         conf,
	}
	return Cluster(qc), nil
}

func (qc *qemuCluster) Machines() []Machine {
	machines := make([]Machine, 0, len(qc.machines))
	for _, m := range qc.machines {
		machines = append(machines, m)
	}
	return machines
}

func (qc *qemuCluster) Destroy() error {
	for _, qm := range qc.machines {
		qm.Destroy()
	}
	return qc.LocalCluster.Destroy()
}

func (qc *qemuCluster) NewMachine(cfg string) (Machine, error) {
	id := uuid.NewV4()

	// hacky solution for cloud config ip substitution
	// NOTE: escaping is not supported
	netif := qc.Dnsmasq.GetInterface("br0")
	ip := strings.Split(netif.DHCPv4[0].String(), "/")[0]
	cfg = strings.Replace(cfg, "$public_ipv4", ip, -1)
	cfg = strings.Replace(cfg, "$private_ipv4", ip, -1)

	cloudConfig, err := config.NewCloudConfig(cfg)
	if err != nil {
		return nil, err
	}

	if err = qc.SSHAgent.UpdateConfig(cloudConfig); err != nil {
		return nil, err
	}

	if cloudConfig.Hostname == "" {
		cloudConfig.Hostname = id.String()[:8]
	}

	configDrive, err := local.NewConfigDrive(cloudConfig)
	if err != nil {
		return nil, err
	}

	qm := &qemuMachine{
		qc:          qc,
		id:          id.String(),
		configDrive: configDrive,
		netif:       netif,
	}

	disk, err := setupDisk(qc.conf.DiskImage)
	if err != nil {
		return nil, err
	}
	defer disk.Close()

	tap, err := qc.NewTap("br0")
	if err != nil {
		return nil, err
	}
	defer tap.Close()

	qmMac := qm.netif.HardwareAddr.String()
	qmCfg := qm.configDrive.Directory
	qm.qemu = qm.qc.NewCommand(
		"qemu-system-x86_64",
		"-machine", "accel=kvm",
		"-cpu", "host",
		"-smp", "2",
		"-m", "1024",
		"-uuid", qm.id,
		"-display", "none",
		"-add-fd", "fd=3,set=1",
		"-drive", "file=/dev/fdset/1,media=disk,if=virtio,format=raw",
		"-netdev", "tap,id=tap,fd=4",
		"-device", "virtio-net,netdev=tap,mac="+qmMac,
		"-fsdev", "local,id=cfg,security_model=none,readonly,path="+qmCfg,
		"-device", "virtio-9p-pci,fsdev=cfg,mount_tag=config-2")

	cmd := qm.qemu.(*local.NsCmd)
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = append(cmd.ExtraFiles, disk)     // fd=3
	cmd.ExtraFiles = append(cmd.ExtraFiles, tap.File) // fd=4

	if err = qm.qemu.Start(); err != nil {
		return nil, err
	}

	// Allow a few authentication failures in case setup is slow.
	sshchecker := func() error {
		qm.sshClient, err = qm.qc.SSHAgent.NewClient(qm.IP())
		if err != nil {
			return err
		}
		return nil
	}

	if err := util.Retry(sshRetries, sshTimeout, sshchecker); err != nil {
		return nil, err
	}

	if err != nil {
		qm.Destroy()
		return nil, err
	}

	out, err := qm.SSH("grep ^ID= /etc/os-release")
	if err != nil {
		qm.Destroy()
		return nil, err
	}

	if !bytes.Equal(out, []byte("ID=coreos")) {
		qm.Destroy()
		return nil, fmt.Errorf("Unexpected SSH output: %s", out)
	}

	qc.machines[qm.ID()] = qm

	return Machine(qm), nil
}

// Copy the base image to a new nameless temporary file.
// cp is used since it supports sparse and reflink.
func setupDisk(imageFile string) (*os.File, error) {
	dstFile, err := ioutil.TempFile("", "mantle-qemu")
	if err != nil {
		return nil, err
	}
	dstFileName := dstFile.Name()
	defer os.Remove(dstFileName)
	dstFile.Close()

	cp := exec.Command("cp", "--force",
		"--sparse=always", "--reflink=auto",
		imageFile, dstFileName)
	cp.Stdout = os.Stdout
	cp.Stderr = os.Stderr

	if err := cp.Run(); err != nil {
		return nil, err
	}

	return os.OpenFile(dstFileName, os.O_RDWR, 0)
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

func (qm *qemuMachine) SSHSession() (*ssh.Session, error) {
	session, err := qm.sshClient.NewSession()
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (qm *qemuMachine) SSH(cmd string) ([]byte, error) {
	session, err := qm.SSHSession()
	if err != nil {
		return []byte{}, err
	}
	defer session.Close()

	session.Stderr = os.Stderr
	out, err := session.Output(cmd)
	out = bytes.TrimSpace(out)
	return out, err
}

func (qm *qemuMachine) StartJournal() error {
	s, err := qm.SSHSession()
	if err != nil {
		return fmt.Errorf("SSH session failed: %v", err)
	}

	s.Stdout = os.Stdout
	s.Stderr = os.Stderr
	go func() {
		s.Run("journalctl -f")
		s.Close()
	}()

	return nil
}

func (qm *qemuMachine) Destroy() error {
	if qm.sshClient != nil {
		qm.sshClient.Close()
	}
	err := qm.qemu.Kill()

	if qm.configDrive != nil {
		err2 := qm.configDrive.Destroy()
		if err == nil && err2 != nil {
			err = err2
		}
	}

	delete(qm.qc.machines, qm.ID())
	return err
}
