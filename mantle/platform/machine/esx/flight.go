// Copyright 2016 CoreOS, Inc.
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

package esx

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/esx"
	"github.com/coreos/mantle/platform/conf"
)

const (
	Platform platform.Name = "esx"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/esx")
)

type flight struct {
	*platform.BaseFlight
	api *esx.API
}

// NewFlight creates an instance of a Flight suitable for spawning
// clusters on VMware ESXi vSphere platform.
func NewFlight(opts *esx.Options) (platform.Flight, error) {
	api, err := esx.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	ef := &flight{
		BaseFlight: bf,
		api:        api,
	}

	return ef, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on VMware ESXi vSphere platform.
func (ef *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(ef.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	ec := &cluster{
		BaseCluster: bc,
		flight:      ef,
	}

	ef.AddCluster(ec)

	return ec, nil
}
