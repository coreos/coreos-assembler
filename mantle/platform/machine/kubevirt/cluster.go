// Copyright 2025 Red Hat
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

package kubevirt

import (
	"crypto/rand"
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

func (kc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return kc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (kc *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform kubevirt does not support additional disks")
	}
	if options.MultiPathDisk {
		return nil, errors.New("platform kubevirt does not support multipathed disks")
	}
	if options.AdditionalNics > 0 {
		return nil, errors.New("platform kubevirt does not support additional nics")
	}
	if options.AppendKernelArgs != "" {
		return nil, errors.New("platform kubevirt does not support appending kernel arguments")
	}
	if options.AppendFirstbootKernelArgs != "" {
		return nil, errors.New("platform kubevirt does not support appending firstboot kernel arguments")
	}

	// Determine cloud-init type: per-machine option takes precedence over CLI flag default
	cloudInitType := options.CloudInitType
	if cloudInitType == "" {
		cloudInitType = kc.flight.api.Opts().CloudInitType
	}

	conf, err := kc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_KUBEVIRT_IPV4_LOCAL}",
		"$private_ipv4": "${COREOS_KUBEVIRT_IPV4_LOCAL}",
	})
	if err != nil {
		return nil, err
	}

	vmName := kc.vmname()

	if err := kc.flight.api.CreateVM(vmName, conf.String(), cloudInitType, options.NetworkData); err != nil {
		return nil, err
	}

	// Set up SSH tunnel via port-forward
	tunnel, err := kc.flight.api.StartPortForward(vmName, 22)
	if err != nil {
		kc.flight.api.DeleteVM(vmName)
		return nil, fmt.Errorf("setting up port-forward for %s: %v", vmName, err)
	}

	mach := &machine{
		cluster: kc,
		vmiName: vmName,
		tunnel:  tunnel,
	}

	mach.dir = filepath.Join(kc.RuntimeConf().OutputDir, mach.ID())
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

	if err := platform.StartMachine(mach, mach.journal); err != nil {
		mach.Destroy()
		return nil, err
	}

	kc.AddMach(mach)
	return mach, nil
}

func (kc *cluster) vmname() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate a random vmname: %v", err))
	}
	return fmt.Sprintf("%s-%x", kc.Name()[0:13], b)
}

func (kc *cluster) Destroy() {
	kc.BaseCluster.Destroy()
	kc.flight.DelCluster(kc)
}
