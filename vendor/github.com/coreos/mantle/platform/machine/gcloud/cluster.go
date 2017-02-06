// Copyright 2015 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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
	"strings"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	api *gcloud.API
}

func NewCluster(opts *gcloud.Options) (platform.Cluster, error) {
	api, err := gcloud.New(opts)
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.BaseName)
	if err != nil {
		return nil, err
	}

	gc := &cluster{
		BaseCluster: bc,
		api:         api,
	}

	return gc, nil
}

// Calling in parallel is ok
func (gc *cluster) NewMachine(userdata string) (platform.Machine, error) {
	// hacky solution for unified ignition metadata variables
	if strings.Contains(userdata, `"ignition":`) {
		userdata = strings.Replace(userdata, "$public_ipv4", "${COREOS_GCE_IP_EXTERNAL_0}", -1)
		userdata = strings.Replace(userdata, "$private_ipv4", "${COREOS_GCE_IP_LOCAL_0}", -1)
	}

	conf, err := conf.New(userdata)
	if err != nil {
		return nil, err
	}

	keys, err := gc.Keys()
	if err != nil {
		return nil, err
	}

	conf.CopyKeys(keys)

	instance, err := gc.api.CreateInstance(conf.String(), keys)
	if err != nil {
		return nil, err
	}

	intip, extip := gcloud.InstanceIPs(instance)

	gm := &machine{
		gc:    gc,
		name:  instance.Name,
		intIP: intip,
		extIP: extip,
	}

	if err := platform.CheckMachine(gm); err != nil {
		gm.Destroy()
		return nil, err
	}

	if err := platform.EnableSelinux(gm); err != nil {
		gm.Destroy()
		return nil, err
	}
	gc.AddMach(gm)

	return gm, nil
}
