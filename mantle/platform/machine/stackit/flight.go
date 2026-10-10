// Copyright 2026 Red Hat
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

package stackit

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/stackit"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const Platform platform.Name = "stackit"

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/machine/stackit")

type flight struct {
	*platform.BaseFlight
	api      *stackit.API
	opts     stackit.Options
	keyAdded bool
}

var _ platform.Flight = (*flight)(nil)

// NewFlight creates a flight for spawning instances on STACKIT.
func NewFlight(opts *stackit.Options) (platform.Flight, error) {
	api, err := stackit.New(opts)
	if err != nil {
		return nil, err
	}
	if err := api.ValidateMachineOptions(); err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	sf := &flight{
		BaseFlight: bf,
		api:        api,
		opts:       *opts,
	}
	sf.opts.SecurityGroups = append([]string(nil), opts.SecurityGroups...)

	keys, err := sf.Keys()
	if err != nil {
		sf.Destroy()
		return nil, err
	}
	if err := sf.api.AddKey(sf.Name(), keys[0].String()); err != nil {
		sf.Destroy()
		return nil, err
	}
	sf.keyAdded = true

	return sf, nil
}

func (sf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(sf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	sc := &cluster{
		BaseCluster: bc,
		flight:      sf,
	}
	sc.networking, err = newClusterNetworking(sf.api, sc.Name(), &sf.opts)
	if err != nil {
		return nil, err
	}
	sf.AddCluster(sc)
	return sc, nil
}

func (sf *flight) ConfigTooLarge(ud conf.UserData) bool {
	// No user-data size limit has been established for STACKIT.
	return false
}

func (sf *flight) Destroy() {
	// Keep the SSH key available until all instances have been destroyed.
	sf.BaseFlight.Destroy()

	if sf.keyAdded {
		if err := sf.api.DeleteKey(sf.Name()); err != nil {
			plog.Errorf("Error deleting key %v: %v", sf.Name(), err)
		} else {
			sf.keyAdded = false
		}
	}
}
