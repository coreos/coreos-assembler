// Copyright 2016 CoreOS, Inc.
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

package aws

import (
	"fmt"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/conf"
)

type Options struct {
	*platform.Options
	AMI           string
	InstanceType  string
	SecurityGroup string
}

type cluster struct {
	*platform.BaseCluster
	api     *aws.API
	options *Options
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on Amazon Web Services' Elastic Compute platform.
//
// NewCluster will consume the environment variables $AWS_REGION,
// $AWS_ACCESS_KEY_ID, and $AWS_SECRET_ACCESS_KEY to determine the region to
// spawn instances in and the credentials to use to authenticate.
func NewCluster(opts *Options) (platform.Cluster, error) {
	api, err := aws.New()
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.BaseName)
	if err != nil {
		return nil, err
	}

	ac := &cluster{
		BaseCluster: bc,
		api:         api,
		options:     opts,
	}

	keys, err := ac.Keys()
	if err != nil {
		return nil, err
	}

	if err := api.AddKey(bc.Name(), keys[0].String()); err != nil {
		return nil, err
	}

	return ac, nil
}

func (ac *cluster) NewMachine(userdata string) (platform.Machine, error) {
	conf, err := conf.New(userdata)
	if err != nil {
		return nil, err
	}

	keys, err := ac.Keys()
	if err != nil {
		return nil, err
	}

	conf.CopyKeys(keys)

	instances, err := ac.api.CreateInstances(ac.options.AMI, ac.Name(), conf.String(), ac.options.InstanceType, ac.options.SecurityGroup, 1, true)

	mach := &machine{
		cluster: ac,
		mach:    instances[0],
	}

	if err := platform.CheckMachine(mach); err != nil {
		return nil, fmt.Errorf("machine %q failed basic checks: %v", mach.ID(), err)
	}

	ac.AddMach(mach)

	return mach, nil
}

func (ac *cluster) Destroy() error {
	if err := ac.api.DeleteKey(ac.Name()); err != nil {
		return err
	}

	return ac.BaseCluster.Destroy()
}
