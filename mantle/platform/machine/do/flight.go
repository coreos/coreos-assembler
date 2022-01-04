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

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/do"
	"github.com/coreos/mantle/platform/conf"
)

const (
	Platform platform.Name = "do"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/do")
)

type flight struct {
	*platform.BaseFlight
	api          *do.API
	sshKeyID     int
	fakeSSHKeyID int
}

func NewFlight(opts *do.Options) (platform.Flight, error) {
	api, err := do.New(opts)
	if err != nil {
		return nil, err
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
	if err != nil {
		return nil, err
	}

	df := &flight{
		BaseFlight: bf,
		api:        api,
	}

	keys, err := df.Keys()
	if err != nil {
		df.Destroy()
		return nil, err
	}
	df.sshKeyID, err = df.api.AddKey(context.TODO(), df.Name(), keys[0].String())
	if err != nil {
		df.Destroy()
		return nil, err
	}

	// The DO API requires us to provide an SSH key for Container Linux
	// droplets.  Create one that can never authenticate.
	key, err := platform.GenerateFakeKey()
	if err != nil {
		df.Destroy()
		return nil, err
	}
	df.fakeSSHKeyID, err = df.api.AddKey(context.TODO(), df.Name()+"-fake", key)
	if err != nil {
		df.Destroy()
		return nil, err
	}

	return df, nil
}

func (df *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(df.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	dc := &cluster{
		BaseCluster: bc,
		flight:      df,
	}
	if !rconf.NoSSHKeyInMetadata {
		dc.sshKeyID = df.sshKeyID
	} else {
		// The DO API requires us to provide an SSH key for
		// Container Linux droplets. Provide one that can never
		// authenticate.
		dc.sshKeyID = df.fakeSSHKeyID
	}

	df.AddCluster(dc)

	return dc, nil
}

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

func (df *flight) Destroy() {
	for _, keyID := range []int{df.sshKeyID, df.fakeSSHKeyID} {
		if keyID == 0 {
			continue
		}
		if err := df.api.DeleteKey(context.TODO(), keyID); err != nil {
			plog.Errorf("Error deleting key %v: %v", keyID, err)
		}
	}

	df.BaseFlight.Destroy()
}
