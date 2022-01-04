// Copyright 2015 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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

package gcloud

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/platform/conf"
)

type flight struct {
	*platform.BaseFlight
	api *gcloud.API
}

const (
	Platform platform.Name = "gcloud"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/gcloud")
)

func NewFlight(opts *gcloud.Options) (platform.Flight, error) {
	api, err := gcloud.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	gf := &flight{
		BaseFlight: bf,
		api:        api,
	}

	return gf, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

func (gf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(gf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	gc := &cluster{
		BaseCluster: bc,
		flight:      gf,
	}

	gf.AddCluster(gc)

	return gc, nil
}
