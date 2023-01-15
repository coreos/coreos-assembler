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

package qemuiso

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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

	mu sync.Mutex
}

func (qc *Cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return qc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (qc *Cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	return qc.NewMachineWithQemuOptions(userdata, platform.QemuMachineOptions{
		MachineOptions: options,
	})
}

func (qc *Cluster) NewMachineWithQemuOptions(userdata *conf.UserData, options platform.QemuMachineOptions) (platform.Machine, error) {
	if options.MultiPathDisk {
		return nil, errors.New("platform qemu-iso does not support multipathed primary disks")
	}
	id := uuid.New()

	dir := filepath.Join(qc.RuntimeConf().OutputDir, id)
	if err := os.Mkdir(dir, 0777); err != nil {
		return nil, err
	}

	// hacky solution for cloud config ip substitution
	// NOTE: escaping is not supported
	qc.mu.Lock()

	conf, err := qc.RenderUserData(userdata, map[string]string{})
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}
	qc.mu.Unlock()

	var confPath string
	if conf.IsIgnition() {
		confPath = filepath.Join(dir, "ignition.json")
		if err := conf.WriteFile(confPath); err != nil {
			return nil, err
		}
	} else if conf.IsEmpty() {
	} else {
		return nil, fmt.Errorf("qemuiso only supports Ignition or empty configs")
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
	builder.ConfigFile = confPath
	defer builder.Close()
	builder.UUID = qm.id
	if qc.flight.opts.Firmware != "" {
		builder.Firmware = qc.flight.opts.Firmware
	}
	builder.Hostname = fmt.Sprintf("qemu%d", qc.BaseCluster.AllocateMachineSerial())
	builder.ConsoleFile = qm.consolePath

	if qc.flight.opts.Memory != "" {
		memory, err := strconv.ParseInt(qc.flight.opts.Memory, 10, 32)
		if err != nil {
			return nil, errors.Wrapf(err, "parsing memory option")
		}
		builder.Memory = int(memory)
	}

	if err := builder.AddIso(qc.flight.opts.IsoPath, "", qc.flight.opts.AsDisk); err != nil {
		return nil, errors.Wrapf(err, "adding ISO image")
	}

	if err = builder.AddDisksFromSpecs(options.AdditionalDisks); err != nil {
		return nil, err
	}

	if len(options.HostForwardPorts) > 0 {
		builder.EnableUsermodeNetworking(options.HostForwardPorts)
	} else {
		h := []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
		}
		builder.EnableUsermodeNetworking(h)
	}

	if options.AdditionalNics > 0 {
		builder.AddAdditionalNics(options.AdditionalNics)
	}
	if options.AppendKernelArgs != "" {
		builder.AppendKernelArgs = options.AppendKernelArgs
	}
	if options.AppendFirstbootKernelArgs != "" {
		builder.AppendFirstbootKernelArgs = options.AppendFirstbootKernelArgs
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

	return qm, nil
}

func (qc *Cluster) Destroy() {
	qc.BaseCluster.Destroy()
	qc.flight.DelCluster(qc)
}
