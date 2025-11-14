// Copyright 2019 Red Hat
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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sync"
	"time"

	"github.com/pborman/uuid"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
)

// Cluster is a local cluster of QEMU-based virtual machines.
//
// XXX: must be exported so that certain QEMU tests can access struct members
// through type assertions.
type Cluster struct {
	*platform.BaseCluster
	flight *flight

	mu          sync.Mutex
	tearingDown bool
}

type BuilderCallbacks struct {
	BuilderInit      func(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error
	SetupDisks       func(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error
	SetupNetwork     func(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error
	OverrideDefaults func(builder *platform.QemuBuilder) error
}

func (qc *Cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return qc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (qc *Cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if options.InstanceType != "" {
		return nil, errors.New("platform qemu does not support changing instance types")
	}
	return qc.NewMachineWithQemuOptions(userdata, platform.QemuMachineOptions{
		MachineOptions: options,
		Firmware:       options.Firmware,
	})
}

func (qc *Cluster) NewMachineWithQemuOptions(userdata *conf.UserData, options platform.QemuMachineOptions) (platform.Machine, error) {
	return qc.NewMachineWithQemuOptionsAndBuilderCallbacks(userdata, options, BuilderCallbacks{
		BuilderInit:      qc.InitDefaultBuilder,
		SetupDisks:       qc.SetupDefaultDisks,
		SetupNetwork:     qc.SetupDefaultNetwork,
		OverrideDefaults: nil,
	})
}

func (qc *Cluster) NewMachineWithQemuOptionsAndBuilderCallbacks(userdata any, options platform.QemuMachineOptions, callbacks BuilderCallbacks) (platform.Machine, error) {
	if callbacks.BuilderInit == nil {
		callbacks.BuilderInit = qc.InitDefaultBuilder
	}
	if callbacks.SetupDisks == nil {
		callbacks.SetupDisks = qc.SetupDefaultDisks
	}
	if callbacks.SetupNetwork == nil {
		callbacks.SetupNetwork = qc.SetupDefaultNetwork
	}

	id := uuid.New()

	dir := filepath.Join(qc.RuntimeConf().OutputDir, id)
	if err := os.Mkdir(dir, 0777); err != nil {
		return nil, err
	}

	conf, confPath, err := qc.ProcessIgnitionConfig(userdata, dir)
	if err != nil {
		return nil, err
	}

	journal, err := platform.NewJournal(dir)
	if err != nil {
		return nil, err
	}

	qm := &machine{
		qc:          qc,
		id:          id,
		journal:     journal,
		consolePath: filepath.Join(dir, "console.txt"),
	}

	builder := platform.NewQemuBuilder()
	defer builder.Close()
	if err := callbacks.BuilderInit(options, builder); err != nil {
		return nil, err
	}
	builder.UUID = qm.id
	builder.ConsoleFile = qm.consolePath
	builder.ConfigFile = confPath
	// This one doesn't support configuring the path because we can't
	// reliably change the Ignition config here...
	for _, path := range qc.flight.opts.BindRO {
		destpathrel := strings.TrimLeft(path, "/")
		builder.MountHost(path, "/kola/host/"+destpathrel, true)
	}

	if err := callbacks.SetupDisks(options, builder); err != nil {
		return nil, err
	}
	if err := callbacks.SetupNetwork(options, builder); err != nil {
		return nil, err
	}
	if callbacks.OverrideDefaults != nil {
		if err := callbacks.OverrideDefaults(builder); err != nil {
			return nil, err
		}
	}
	// S390x specific stuff
	if qc.flight.opts.SecureExecution {
		if err := builder.SetSecureExecution(qc.flight.opts.SecureExecutionIgnitionPubKey, qc.flight.opts.SecureExecutionHostKey, conf); err != nil {
			return nil, err
		}
	}
	if qc.flight.opts.Cex || options.Cex {
		if err := builder.AddCexDevice(); err != nil {
			return nil, err
		}
	}

	inst, err := builder.Exec()
	if err != nil {
		return nil, err
	}
	qm.inst = inst

	err = util.Retry(6, 5*time.Second, func() error {
		var err error
		qm.ip, err = inst.SSHAddress()
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Run StartMachine, which blocks on the machine being booted up enough
	// for SSH access, but only if the caller didn't tell us not to.
	if !options.SkipStartMachine {
		if err := platform.StartMachine(qm, qm.journal); err != nil {
			qm.Destroy()
			return nil, err
		}
	}

	qc.AddMach(qm)

	// In this flow, nothing actually Wait()s for the QEMU process. Let's do it here
	// and print something if it exited unexpectedly. Ideally in the future, this
	// interface allows the test harness to provide e.g. a channel we can signal on so
	// it knows to stop the test once QEMU dies.
	go func() {
		err := inst.Wait()
		if err != nil && !qc.tearingDown {
			plog.Errorf("QEMU process finished abnormally: %v", err)
		}
	}()

	return qm, nil
}

func (qc *Cluster) Destroy() {
	qc.tearingDown = true
	qc.BaseCluster.Destroy()
	qc.flight.DelCluster(qc)
}

func (qc *Cluster) ProcessIgnitionConfig(cfg any, dir string) (*conf.Conf, string, error) {
	var config *conf.Conf
	var confPath string
	var err error
	switch p := cfg.(type) {
	case *conf.UserData:
		config, err = qc.RenderUserDataForCloudIpSubstitution(p)
		if err != nil {
			return nil, "", err
		}
	case *conf.Conf:
		config = p
	default:
		return nil, "", fmt.Errorf("unknown config pointer type: %T", p)
	}
	if config != nil {
		confPath, err = qc.WriteIgnitionConfigToDir(config, dir)
		if err != nil {
			return nil, "", err
		}
	}
	return config, confPath, nil
}

// hacky solution for cloud config ip substitution
// NOTE: escaping is not supported
func (qc *Cluster) RenderUserDataForCloudIpSubstitution(userdata *conf.UserData) (conf *conf.Conf, err error) {
	qc.mu.Lock()
	conf, err = qc.RenderUserData(userdata, map[string]string{})
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}
	qc.mu.Unlock()
	return conf, nil
}

func (qc *Cluster) WriteIgnitionConfigToDir(conf *conf.Conf, dir string) (confPath string, err error) {
	if conf.IsIgnition() {
		confPath = filepath.Join(dir, "ignition.json")
		if err = conf.WriteFile(confPath); err != nil {
			return confPath, err
		}
	} else if !conf.IsEmpty() {
		return confPath, fmt.Errorf("qemu only supports Ignition or empty configs")
	}
	return confPath, nil
}

func (qc *Cluster) InitDefaultBuilder(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
	if options.DisablePDeathSig {
		builder.Pdeathsig = false
	}
	if qc.flight.opts.Arch != "" {
		if err := builder.SetArchitecture(qc.flight.opts.Arch); err != nil {
			return err
		}
	}
	if qc.flight.opts.Firmware != "" {
		builder.Firmware = qc.flight.opts.Firmware
	}
	if qc.flight.opts.Memory != "" {
		memory, err := strconv.ParseInt(qc.flight.opts.Memory, 10, 32)
		if err != nil {
			return errors.Wrapf(err, "parsing memory option")
		}
		builder.MemoryMiB = int(memory)
	} else if options.MinMemory != 0 {
		builder.MemoryMiB = options.MinMemory
	} else if qc.flight.opts.SecureExecution {
		builder.MemoryMiB = 4096 // SE needs at least 4GB
	}
	builder.Swtpm = qc.flight.opts.Swtpm
	builder.Hostname = fmt.Sprintf("qemu%d", qc.BaseCluster.AllocateMachineSerial())
	if options.Firmware != "" {
		builder.Firmware = options.Firmware
	}
	if options.AppendKernelArgs != "" {
		builder.AppendKernelArgs = options.AppendKernelArgs
	}
	if options.AppendFirstbootKernelArgs != "" {
		builder.AppendFirstbootKernelArgs = options.AppendFirstbootKernelArgs
	}

	return nil
}

func (qc *Cluster) SetupDefaultDisks(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
	var primaryDisk platform.Disk
	if options.PrimaryDisk != "" {
		diskp, err := platform.ParseDisk(options.PrimaryDisk, true)
		if err != nil {
			return errors.Wrapf(err, "parsing primary disk spec '%s'", options.PrimaryDisk)
		}
		primaryDisk = *diskp
	}
	if qc.flight.opts.Nvme || options.Nvme {
		primaryDisk.Channel = "nvme"
	}
	if qc.flight.opts.Native4k {
		primaryDisk.SectorSize = 4096
	} else if qc.flight.opts.Disk512e {
		primaryDisk.SectorSize = 4096
		primaryDisk.LogicalSectorSize = 512
	}
	if options.MultiPathDisk || qc.flight.opts.MultiPathDisk {
		primaryDisk.MultiPathDisk = true
	}
	if options.MinDiskSize > 0 {
		primaryDisk.Size = fmt.Sprintf("%dG", options.MinDiskSize)
	} else if qc.flight.opts.DiskSize != "" {
		primaryDisk.Size = qc.flight.opts.DiskSize
	}
	primaryDisk.BackingFile = qc.flight.opts.DiskImage
	if options.OverrideBackingFile != "" {
		primaryDisk.BackingFile = options.OverrideBackingFile
	}
	if err := builder.AddBootDisk(&primaryDisk); err != nil {
		return err
	}
	if err := builder.AddDisksFromSpecs(options.AdditionalDisks); err != nil {
		return err
	}
	return nil
}

func (qc *Cluster) SetupDefaultNetwork(options platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
	if len(options.HostForwardPorts) > 0 {
		builder.EnableUsermodeNetworking(options.HostForwardPorts, "")
	} else {
		h := []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
		}
		builder.EnableUsermodeNetworking(h, "")
	}
	if options.AdditionalNics > 0 {
		builder.AddAdditionalNics(options.AdditionalNics)
	}
	if !qc.RuntimeConf().InternetAccess {
		builder.RestrictNetworking = true
	}
	return nil
}
