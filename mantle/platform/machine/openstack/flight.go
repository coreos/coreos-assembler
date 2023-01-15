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
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/openstack"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const (
	Platform platform.Name = "openstack"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/machine/openstack")
)

type flight struct {
	*platform.BaseFlight
	api      *openstack.API
	keyAdded bool
}

// NewFlight creates an instance of a Flight suitable for spawning
// instances on the OpenStack platform.
func NewFlight(opts *openstack.Options) (platform.Flight, error) {
	api, err := openstack.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	of := &flight{
		BaseFlight: bf,
		api:        api,
	}

	keys, err := of.Keys()
	if err != nil {
		of.Destroy()
		return nil, err
	}

	if err := api.AddKey(of.Name(), keys[0].String()); err != nil {
		of.Destroy()
		return nil, err
	}
	of.keyAdded = true

	return of, nil
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on the OpenStack platform.
func (of *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(of.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	oc := &cluster{
		BaseCluster: bc,
		flight:      of,
	}

	of.AddCluster(oc)

	return oc, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

func (of *flight) Destroy() {
	if of.keyAdded {
		if err := of.api.DeleteKey(of.Name()); err != nil {
			plog.Errorf("Error deleting key %v: %v", of.Name(), err)
		}
	}

	of.BaseFlight.Destroy()
}
