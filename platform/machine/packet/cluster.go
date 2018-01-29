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

package packet

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/pkg/capnslog"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/packet"
	"github.com/coreos/mantle/platform/conf"
)

const (
	Platform platform.Name = "packet"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/packet")
)

type cluster struct {
	*platform.BaseCluster
	api      *packet.API
	sshKeyID string
}

func NewCluster(opts *packet.Options, rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	api, err := packet.New(opts)
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.BaseName, rconf, Platform, ctplatform.Packet)
	if err != nil {
		return nil, err
	}

	var keyID string
	if !rconf.NoSSHKeyInMetadata {
		keys, err := bc.Keys()
		if err != nil {
			return nil, err
		}

		keyID, err = api.AddKey(bc.Name(), keys[0].String())
		if err != nil {
			return nil, err
		}
	}

	pc := &cluster{
		BaseCluster: bc,
		api:         api,
		sshKeyID:    keyID,
	}

	return pc, nil
}

func (pc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	conf, err := pc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_PACKET_IPV4_PUBLIC_0}",
		"$private_ipv4": "${COREOS_PACKET_IPV4_PRIVATE_0}",
	})
	if err != nil {
		return nil, err
	}

	vmname := pc.vmname()
	// Stream the console somewhere temporary until we have a machine ID
	consolePath := filepath.Join(pc.RuntimeConf().OutputDir, "console-"+vmname+".txt")
	var cons *console
	var pcons packet.Console // need a nil interface value if unused
	if pc.sshKeyID != "" {
		// We can only read the console if Packet has our SSH key
		f, err := os.OpenFile(consolePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if err != nil {
			return nil, err
		}
		cons = &console{
			pc:   pc,
			f:    f,
			done: make(chan interface{}),
		}
		pcons = cons
	}

	// CreateDevice unconditionally closes console when done with it
	device, err := pc.api.CreateDevice(vmname, conf, pcons)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: pc,
		device:  device,
		console: cons,
	}
	mach.publicIP = pc.api.GetDeviceAddress(device, 4, true)
	mach.privateIP = pc.api.GetDeviceAddress(device, 4, false)
	if mach.publicIP == "" || mach.privateIP == "" {
		mach.Destroy()
		return nil, fmt.Errorf("couldn't find IP addresses for device")
	}

	dir := filepath.Join(pc.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	if cons != nil {
		if err := os.Rename(consolePath, filepath.Join(dir, "console.txt")); err != nil {
			mach.Destroy()
			return nil, err
		}
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

	if err := platform.StartMachine(mach, mach.journal); err != nil {
		mach.Destroy()
		return nil, err
	}

	pc.AddMach(mach)

	return mach, nil
}

func (pc *cluster) vmname() string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", pc.Name()[0:13], b)
}

func (pc *cluster) Destroy() {
	if pc.sshKeyID != "" {
		if err := pc.api.DeleteKey(pc.sshKeyID); err != nil {
			plog.Errorf("Error deleting key %v: %v", pc.sshKeyID, err)
		}
	}

	pc.BaseCluster.Destroy()
}
