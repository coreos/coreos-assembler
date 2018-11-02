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

package do

import (
	"context"

	"github.com/coreos/pkg/capnslog"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/do"
)

const (
	Platform platform.Name = "do"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/do")
)

type flight struct {
	*platform.BaseFlight
	api *do.API
}

func NewFlight(opts *do.Options) (platform.Flight, error) {
	api, err := do.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform, ctplatform.DO)
	if err != nil {
		return nil, err
	}

	return &flight{
		BaseFlight: bf,
		api:        api,
	}, nil
}

func (df *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(df.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	var key string
	if !rconf.NoSSHKeyInMetadata {
		keys, err := bc.Keys()
		if err != nil {
			return nil, err
		}
		key = keys[0].String()
	} else {
		// The DO API requires us to provide an SSH key for
		// Container Linux droplets. Provide one that can never
		// authenticate.
		key, err = do.GenerateFakeKey()
		if err != nil {
			return nil, err
		}
	}
	keyID, err := df.api.AddKey(context.TODO(), bc.Name(), key)
	if err != nil {
		return nil, err
	}

	dc := &cluster{
		BaseCluster: bc,
		flight:      df,
		sshKeyID:    keyID,
	}

	df.AddCluster(dc)

	return dc, nil
}
