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
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const (
	Platform platform.Name = "qemu"
)

// Options contains QEMU-specific options for the flight.
type Options struct {
	// DiskImage is the full path to the disk image to boot in QEMU.
	DiskImage string
	// DiskSize if non-empty will expand the disk
	DiskSize string
	// DriveOpts is arbitrary comma-separated list of options
	DriveOpts string
	// Firmware will be passed to qemu
	Firmware string
	Memory   string
	Arch     string

	NbdDisk       bool
	MultiPathDisk bool
	Native4k      bool
	Nvme          bool

	//Option to create a temporary software TPM - true by default
	Swtpm bool

	//IBM Secure Execution
	SecureExecution               bool
	SecureExecutionIgnitionPubKey string
	SecureExecutionHostKey        string

	*platform.Options
}

type flight struct {
	*platform.BaseFlight
	opts *Options
}

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/machine/qemu")
)

func NewFlight(opts *Options) (platform.Flight, error) {
	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	qf := &flight{
		BaseFlight: bf,
		opts:       opts,
	}

	return qf, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

// NewCluster creates a Cluster instance, suitable for running virtual
// machines in QEMU.
func (qf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(qf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	qc := &Cluster{
		BaseCluster: bc,
		flight:      qf,
	}

	qf.AddCluster(qc)

	return qc, nil
}
