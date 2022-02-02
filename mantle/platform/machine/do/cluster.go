// Copyright 2017 CoreOS, Inc.
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

package do

import (
	"context"
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
	flight   *flight
	sshKeyID int
}

func (dc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return dc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (dc *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform do does not yet support additional disks")
	}
	if options.MultiPathDisk {
		return nil, errors.New("platform do does not support multipathed disks")
	}
	if options.AdditionalNics > 0 {
		return nil, errors.New("platform do does not support additional nics")
	}
	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform do does not support appending kernel arguments")
	}
	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform do does not support appending firstboot kernel arguments")
	}

	conf, err := dc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_DIGITALOCEAN_IPV4_PUBLIC_0}",
		"$private_ipv4": "${COREOS_DIGITALOCEAN_IPV4_PRIVATE_0}",
	})
	if err != nil {
		return nil, err
	}

	droplet, err := dc.flight.api.CreateDroplet(context.TODO(), dc.vmname(), dc.sshKeyID, conf.String())
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: dc,
		droplet: droplet,
	}
	mach.publicIP, err = droplet.PublicIPv4()
	if mach.publicIP == "" || err != nil {
		mach.Destroy()
		return nil, fmt.Errorf("couldn't get public IP address for droplet: %v", err)
	}
	mach.privateIP, err = droplet.PrivateIPv4()
	if mach.privateIP == "" || err != nil {
		mach.Destroy()
		return nil, fmt.Errorf("couldn't get private IP address for droplet: %v", err)
	}

	dir := filepath.Join(dc.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	confPath := filepath.Join(dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		mach.Destroy()
		return nil, err
	}

	if mach.journal, err = platform.NewJournal(dir); err != nil {
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

	dc.AddMach(mach)

	return mach, nil
}

func (dc *cluster) vmname() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		plog.Errorf("failed to generate a random vmname: %v", err)
	}
	return fmt.Sprintf("%s-%x", dc.Name()[0:13], b)
}

func (dc *cluster) Destroy() {
	dc.BaseCluster.Destroy()
	dc.flight.DelCluster(dc)
}
