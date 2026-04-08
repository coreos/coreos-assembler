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
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/platform"
	kubevirtapi "github.com/coreos/coreos-assembler/mantle/platform/api/kubevirt"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const (
	Platform platform.Name = "kubevirt"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/machine/kubevirt")
)

type flight struct {
	*platform.BaseFlight
	api *kubevirtapi.API
}

// NewFlight creates an instance of a Flight suitable for spawning
// instances on the KubeVirt platform.
func NewFlight(opts *kubevirtapi.Options) (platform.Flight, error) {
	api, err := kubevirtapi.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	kf := &flight{
		BaseFlight: bf,
		api:        api,
	}

	return kf, nil
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on the KubeVirt platform.
func (kf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(kf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	kc := &cluster{
		BaseCluster: bc,
		flight:      kf,
	}

	kf.AddCluster(kc)
	return kc, nil
}

func (kf *flight) ConfigTooLarge(ud conf.UserData) bool {
	return false
}

func (kf *flight) Destroy() {
	kf.BaseFlight.Destroy()
}
