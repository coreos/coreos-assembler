// Copyright 2015 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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

package gcloud

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh/agent"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/gcloud"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

func (gc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return gc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (gc *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform gce does not yet support additional disks")
	}
	if options.MultiPathDisk {
		return nil, errors.New("platform gce does not support multipathed disks")
	}
	if options.AdditionalNics > 0 {
		return nil, errors.New("platform gce does not support additional nics")
	}
	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform gce does not support appending kernel arguments")
	}
	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform gce does not support appending firstboot kernel arguments")
	}

	conf, err := gc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_GCE_IP_EXTERNAL_0}",
		"$private_ipv4": "${COREOS_GCE_IP_LOCAL_0}",
	})
	if err != nil {
		return nil, err
	}

	var keys []*agent.Key
	if !gc.RuntimeConf().NoSSHKeyInMetadata {
		keys, err = gc.Keys()
		if err != nil {
			return nil, err
		}
	}

	instance, err := gc.flight.api.CreateInstance(conf.String(), keys, !gc.RuntimeConf().NoInstanceCreds)
	if err != nil {
		return nil, err
	}

	intip, extip := gcloud.InstanceIPs(instance)

	gm := &machine{
		gc:    gc,
		name:  instance.Name,
		intIP: intip,
		extIP: extip,
	}

	gm.dir = filepath.Join(gc.RuntimeConf().OutputDir, gm.ID())
	if err := os.Mkdir(gm.dir, 0777); err != nil {
		gm.Destroy()
		return nil, err
	}

	confPath := filepath.Join(gm.dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		gm.Destroy()
		return nil, err
	}

	if gm.journal, err = platform.NewJournal(gm.dir); err != nil {
		gm.Destroy()
		return nil, err
	}

	// Run StartMachine, which blocks on the machine being booted up enough
	// for SSH access, but only if the caller didn't tell us not to.
	if !options.SkipStartMachine {
		if err := platform.StartMachine(gm, gm.journal); err != nil {
			gm.Destroy()
			return nil, err
		}
	}

	gc.AddMach(gm)

	return gm, nil
}

func (gc *cluster) Destroy() {
	gc.BaseCluster.Destroy()
	gc.flight.DelCluster(gc)
}
