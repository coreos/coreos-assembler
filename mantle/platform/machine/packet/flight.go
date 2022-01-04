// Copyright 2017 CoreOS, Inc.
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

package packet

import (
	"github.com/coreos/pkg/capnslog"

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

type flight struct {
	*platform.BaseFlight
	api      *packet.API
	sshKeyID string
}

func NewFlight(opts *packet.Options) (platform.Flight, error) {
	api, err := packet.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	pf := &flight{
		BaseFlight: bf,
		api:        api,
	}

	keys, err := pf.Keys()
	if err != nil {
		pf.Destroy()
		return nil, err
	}
	pf.sshKeyID, err = pf.api.AddKey(pf.Name(), keys[0].String())
	if err != nil {
		pf.Destroy()
		return nil, err
	}

	return pf, nil
}

func (pf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(pf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	pc := &cluster{
		BaseCluster: bc,
		flight:      pf,
	}
	if !rconf.NoSSHKeyInMetadata {
		pc.sshKeyID = pf.sshKeyID
	}

	pf.AddCluster(pc)

	return pc, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

func (pf *flight) Destroy() {
	if pf.sshKeyID != "" {
		if err := pf.api.DeleteKey(pf.sshKeyID); err != nil {
			plog.Errorf("Error deleting key %v: %v", pf.sshKeyID, err)
		}
	}

	pf.BaseFlight.Destroy()
}
