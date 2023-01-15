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

package aws

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

const MaxUserDataSize = 16384

func (ac *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return ac.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (ac *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform aws does not yet support additional disks")
	}

	if options.MultiPathDisk {
		return nil, errors.New("platform aws does not support multipathed disks")
	}

	if options.AdditionalNics > 0 {
		return nil, errors.New("platform aws does not support additional nics")
	}

	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform aws does not support appending kernel arguments")
	}

	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform aws does not support appending firstboot kernel arguments")
	}

	conf, err := ac.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_EC2_IPV4_PUBLIC}",
		"$private_ipv4": "${COREOS_EC2_IPV4_LOCAL}",
	})
	if err != nil {
		return nil, err
	}

	var keyname string
	if !ac.RuntimeConf().NoSSHKeyInMetadata {
		keyname = ac.flight.Name()
	}
	// Compress via gzip if needed
	ud := conf.String()
	encoding := base64.StdEncoding.EncodeToString([]byte(ud))
	if len([]byte(encoding)) > MaxUserDataSize {
		ud, err = conf.MaybeCompress()
		if err != nil {
			return nil, err
		}
		// Check if config is still too large
		encoding = base64.StdEncoding.EncodeToString([]byte(ud))
		if len([]byte(encoding)) > MaxUserDataSize {
			fmt.Printf("WARNING: compressed userdata exceeds expected limit of %d\n", MaxUserDataSize)
		}
	}
	instances, err := ac.flight.api.CreateInstances(ac.Name(), keyname, ud, 1, int64(options.MinDiskSize), !ac.RuntimeConf().NoInstanceCreds)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: ac,
		mach:    instances[0],
	}

	mach.dir = filepath.Join(ac.RuntimeConf().OutputDir, mach.ID())
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

	ac.AddMach(mach)

	return mach, nil
}

func (ac *cluster) Destroy() {
	ac.BaseCluster.Destroy()
	ac.flight.DelCluster(ac)
}
