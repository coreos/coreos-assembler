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
	"sync/atomic"
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

	// Use atomic.Bool to prevent race conditions
	tearingDown atomic.Bool
}

// MachineBuilder provides hooks to customize machine creation.
// All fields are optional; if nil, default implementations will be used.
type MachineBuilder struct {
	// InitBuilder configures the QemuBuilder with architecture, firmware, memory, etc.
	InitBuilder func(options platform.MachineOptions, builder *platform.QemuBuilder) error
	// SetupDisks configures the primary disk and any additional disks.
	SetupDisks func(options platform.MachineOptions, builder *platform.QemuBuilder) error
	// SetupNetwork configures networking including port forwarding and additional NICs.
	SetupNetwork func(options platform.MachineOptions, builder *platform.QemuBuilder) error
}

func (qc *Cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return qc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (qc *Cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if options.InstanceType != "" {
		return nil, errors.New("platform qemu does not support changing instance types")
	}
	return qc.NewMachineWithBuilder(userdata, options, nil)
}

// NewMachineWithBuilder creates a new machine with custom builder hooks.
// If builder is nil or any of its fields are nil, default implementations are used.
func (qc *Cluster) NewMachineWithBuilder(userdata any, options platform.MachineOptions, builder *MachineBuilder) (platform.Machine, error) {
	// Use default builder if none provided
	builder = qc.ensureBuilderDefaults(builder)

	qm, config, err := qc.createMachine(userdata)
	if err != nil {
		return nil, err
	}

	qemuBuilder := platform.NewQemuBuilder()
	qemuBuilder.SetConfig(config)
	defer qemuBuilder.Close()
	if err := builder.InitBuilder(options, qemuBuilder); err != nil {
		return nil, err
	}

	// If requested, bind mount Host (COSA) directories into the machine for use.
	// These could either come in as MachineOptions OR via the flight options
	// (CLI --qemu-bind-ro option).
	//
	// One example where this is useful is using the host environment (COSA)
	// as a read-only rootfs for quickly starting a container. This use was originally
	// pioneered in testiso in [1].
	// [1] https://github.com/coreos/coreos-assembler/commit/8dbfe3ea8b8f571e732e8cc0ab307e983a0be1f3
	for _, mountpair := range append(qc.flight.opts.BindRO, options.BindMountHostRO...) {
		src, dest, err := platform.ParseBindOpt(mountpair)
		if err != nil {
			return nil, err
		}
		readonly := true
		qemuBuilder.MountHost(src, dest, readonly)
		config.MountHost(dest, readonly)
	}

	qemuBuilder.UUID = qm.id
	qemuBuilder.ConsoleFile = qm.consolePath
	qemuBuilder.NumaNodes = options.NumaNodes

	if err := builder.SetupDisks(options, qemuBuilder); err != nil {
		return nil, err
	}
	if err := builder.SetupNetwork(options, qemuBuilder); err != nil {
		return nil, err
	}

	// S390x specific stuff
	if qc.flight.opts.SecureExecution {
		if err := qemuBuilder.SetSecureExecution(qc.flight.opts.SecureExecutionIgnitionPubKey, qc.flight.opts.SecureExecutionHostKey, config); err != nil {
			return nil, err
		}
	}
	if qc.flight.opts.Cex || options.Cex {
		if err := qemuBuilder.AddCexDevice(); err != nil {
			return nil, err
		}
	}

	inst, err := qemuBuilder.Exec()
	if err != nil {
		return nil, err
	}
	qm.inst = inst

	if qemuBuilder.UsermodeNetworking {
		if err := qc.waitForSSHAddress(qm, inst); err != nil {
			return nil, err
		}
	}

	// Run StartMachine, which blocks on the machine being booted up enough
	// for SSH access.
	if err := platform.StartMachine(qm, qm.journal); err != nil {
		qm.Destroy()
		return nil, err
	}

	qc.AddMach(qm)

	// In this flow, nothing actually Wait()s for the QEMU process. Let's do it here
	// and print something if it exited unexpectedly. Ideally in the future, this
	// interface allows the test harness to provide e.g. a channel we can signal on so
	// it knows to stop the test once QEMU dies.
	go func() {
		err := inst.Wait()
		if err != nil && !qc.tearingDown.Load() {
			plog.Errorf("QEMU process finished abnormally: %v", err)
		}
	}()

	return qm, nil
}

func (qc *Cluster) Destroy() {
	qc.tearingDown.Store(true)
	qc.BaseCluster.Destroy()
	qc.flight.DelCluster(qc)
}

func (qc *Cluster) RenderUserDataIfNeeded(userdata any) (*conf.Conf, error) {
	if userdata == nil {
		return nil, nil
	}
	var config *conf.Conf
	var err error
	// Some callers provide the config directly rather than something
	// that needs to be rendered. Render the userdata into a config
	// if needed.
	switch c := userdata.(type) {
	case *conf.UserData:
		config, err = qc.RenderUserData(c, map[string]string{})
		if err != nil {
			return nil, err
		}
	case *conf.Conf:
		config = c // Already rendered. Just pass through what was provided.
	default:
		return nil, fmt.Errorf("unknown config pointer type: %T", c)
	}
	return config, nil
}

// ensures all builder callbacks have default implementations.
func (qc *Cluster) ensureBuilderDefaults(builder *MachineBuilder) *MachineBuilder {
	if builder == nil {
		builder = &MachineBuilder{}
	}

	if builder.InitBuilder == nil {
		builder.InitBuilder = qc.InitDefaultBuilder
	}
	if builder.SetupDisks == nil {
		builder.SetupDisks = qc.SetupDefaultDisks
	}
	if builder.SetupNetwork == nil {
		builder.SetupNetwork = qc.SetupDefaultNetwork
	}

	return builder
}

// createMachine creates a new machine instance with its directory, config, and journal.
func (qc *Cluster) createMachine(userdata any) (*machine, *conf.Conf, error) {
	id := uuid.New()

	dir := filepath.Join(qc.RuntimeConf().OutputDir, id)
	if err := os.Mkdir(dir, 0777); err != nil {
		return nil, nil, err
	}

	config, err := qc.RenderUserDataIfNeeded(userdata)
	if err != nil {
		return nil, nil, err
	}

	journal, err := platform.NewJournal(dir)
	if err != nil {
		return nil, nil, err
	}

	qm := &machine{
		qc:          qc,
		id:          id,
		journal:     journal,
		consolePath: filepath.Join(dir, "console.txt"),
	}

	return qm, config, nil
}

// waitForSSHAddress waits for the machine to provide an SSH address.
func (qc *Cluster) waitForSSHAddress(qm *machine, inst *platform.QemuInstance) error {
	return util.Retry(6, 5*time.Second, func() error {
		var err error
		qm.ip, err = inst.SSHAddress()
		return err
	})
}

func (qc *Cluster) InitDefaultBuilder(options platform.MachineOptions, builder *platform.QemuBuilder) error {
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

func (qc *Cluster) SetupDefaultDisks(options platform.MachineOptions, builder *platform.QemuBuilder) error {
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

func (qc *Cluster) SetupDefaultNetwork(options platform.MachineOptions, builder *platform.QemuBuilder) error {
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

// Instance returns the underlying QemuInstance for a given Machine.
// This allows tests to access QEMU-specific functionality.
func (qc *Cluster) Instance(m platform.Machine) *platform.QemuInstance {
	qm, ok := m.(*machine)
	if !ok {
		return nil
	}
	return qm.inst
}
