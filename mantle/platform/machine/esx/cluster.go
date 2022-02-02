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

package esx

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/platform"
	platformConf "github.com/coreos/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

func (ec *cluster) vmname() string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", ec.Name(), b)
}

func (ec *cluster) NewMachine(userdata *platformConf.UserData) (platform.Machine, error) {
	return ec.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (ec *cluster) NewMachineWithOptions(userdata *platformConf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform esx does not yet support additional disks")
	}
	if options.MultiPathDisk {
		return nil, errors.New("platform esx does not support multipathed disks")
	}
	if options.AdditionalNics > 0 {
		return nil, errors.New("platform esx does not support additional nics")
	}
	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform esx does not support appending kernel arguments")
	}
	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform esx does not support appending firstboot kernel arguments")
	}

	conf, err := ec.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_ESX_IPV4_PUBLIC_0}",
		"$private_ipv4": "${COREOS_ESX_IPV4_PRIVATE_0}",
	})
	if err != nil {
		return nil, err
	}

	conf.AddSystemdUnit("coreos-metadata.service", `[Unit]
Description=VMware metadata agent

[Service]
Type=oneshot
Environment=OUTPUT=/run/metadata/coreos
ExecStart=/usr/bin/mkdir --parent /run/metadata
ExecStart=/usr/bin/bash -c 'echo "COREOS_ESX_IPV4_PRIVATE_0=$(ip addr show ens192 | grep -Po "inet \K[\d.]+")\nCOREOS_ESX_IPV4_PUBLIC_0=$(ip addr show ens192 | grep -Po "inet \K[\d.]+")" > ${OUTPUT}'`, platformConf.NoState)

	instance, err := ec.flight.api.CreateDevice(ec.vmname(), conf)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: ec,
		mach:    instance,
	}

	mach.dir = filepath.Join(ec.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(mach.dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	confPath := filepath.Join(mach.dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		mach.Destroy()
		return nil, err
	}

	if mach.journal, err = platform.NewJournal(mach.dir); err != nil {
		mach.Destroy()
		return nil, err
	}

	// Run StartMachine, which blocks on the machine being booted up enough
	// for SSH access, but only if the caller didn't tell us not to.
	if !options.SkipStartMachine {
		if err := platform.StartMachine(mach, mach.journal); err != nil {
			mach.Destroy()
			return nil, err
		}
	}

	ec.AddMach(mach)

	return mach, nil
}

func (ec *cluster) Destroy() {
	ec.BaseCluster.Destroy()
	ec.flight.DelCluster(ec)
}
