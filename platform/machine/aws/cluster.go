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
	"os"
	"path/filepath"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/conf"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/aws")
)

type cluster struct {
	*platform.BaseCluster
	api *aws.API
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on Amazon Web Services' Elastic Compute platform.
//
// NewCluster will consume the environment variables $AWS_REGION,
// $AWS_ACCESS_KEY_ID, and $AWS_SECRET_ACCESS_KEY to determine the region to
// spawn instances in and the credentials to use to authenticate.
func NewCluster(opts *aws.Options, rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	api, err := aws.New(opts)
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.BaseName, rconf, ctplatform.EC2)
	if err != nil {
		return nil, err
	}

	ac := &cluster{
		BaseCluster: bc,
		api:         api,
	}

	if !rconf.NoSSHKeyInMetadata {
		keys, err := ac.Keys()
		if err != nil {
			return nil, err
		}

		if err := api.AddKey(bc.Name(), keys[0].String()); err != nil {
			return nil, err
		}
	}

	return ac, nil
}

func (ac *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	conf, err := ac.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_EC2_IPV4_PUBLIC}",
		"$private_ipv4": "${COREOS_EC2_IPV4_LOCAL}",
	})
	if err != nil {
		return nil, err
	}

	var keyname string
	if !ac.RuntimeConf().NoSSHKeyInMetadata {
		keyname = ac.Name()
	}
	instances, err := ac.api.CreateInstances(ac.Name(), keyname, conf.String(), 1)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: ac,
		mach:    instances[0],
	}

	mach.dir = filepath.Join(ac.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(mach.dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	confPath := filepath.Join(mach.dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		mach.Destroy()
		return nil, err
	}

	if mach.journal, err = platform.NewJournal(mach.dir); err != nil {
		mach.Destroy()
		return nil, err
	}

	if err := platform.StartMachine(mach, mach.journal, ac.RuntimeConf()); err != nil {
		mach.Destroy()
		return nil, err
	}

	ac.AddMach(mach)

	return mach, nil
}

func (ac *cluster) Destroy() {
	if !ac.RuntimeConf().NoSSHKeyInMetadata {
		if err := ac.api.DeleteKey(ac.Name()); err != nil {
			plog.Errorf("Error deleting key %v: %v", ac.Name(), err)
		}
	}

	ac.BaseCluster.Destroy()
}
