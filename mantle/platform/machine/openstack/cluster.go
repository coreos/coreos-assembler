// Copyright 2018 Red Hat
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

package openstack

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

func (oc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return oc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (oc *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform openstack does not yet support additional disks")
	}
	if options.MultiPathDisk {
		return nil, errors.New("platform openstack does not support multipathed disks")
	}
	if options.AdditionalNics > 0 {
		return nil, errors.New("platform openstack does not support additional nics")
	}
	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform openstack does not support appending kernel arguments")
	}
	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform openstack does not support appending firstboot kernel arguments")
	}

	conf, err := oc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_OPENSTACK_IPV4_PUBLIC}",
		"$private_ipv4": "${COREOS_OPENSTACK_IPV4_LOCAL}",
	})
	if err != nil {
		return nil, err
	}

	var keyname string
	if !oc.RuntimeConf().NoSSHKeyInMetadata {
		keyname = oc.flight.Name()
	}
	instance, err := oc.flight.api.CreateServer(oc.vmname(), keyname, conf.String())
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: oc,
		mach:    instance,
	}

	mach.dir = filepath.Join(oc.RuntimeConf().OutputDir, mach.ID())
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

	oc.AddMach(mach)

	return mach, nil
}

func (oc *cluster) vmname() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		plog.Errorf("failed to generate a random vmname: %v", err)
	}
	return fmt.Sprintf("%s-%x", oc.Name()[0:13], b)
}

func (oc *cluster) Destroy() {
	oc.BaseCluster.Destroy()
	oc.flight.DelCluster(oc)
}
