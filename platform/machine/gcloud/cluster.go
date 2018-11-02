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
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh/agent"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

// Calling in parallel is ok
func (gc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	conf, err := gc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_GCE_IP_EXTERNAL_0}",
		"$private_ipv4": "${COREOS_GCE_IP_LOCAL_0}",
	})
	if err != nil {
		return nil, err
	}

	var keys []*agent.Key
	if !gc.RuntimeConf().NoSSHKeyInMetadata {
		keys, err = gc.Keys()
		if err != nil {
			return nil, err
		}
	}

	instance, err := gc.flight.api.CreateInstance(conf.String(), keys)
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

	gm.dir = filepath.Join(gc.RuntimeConf().OutputDir, gm.ID())
	if err := os.Mkdir(gm.dir, 0777); err != nil {
		gm.Destroy()
		return nil, err
	}

	confPath := filepath.Join(gm.dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		gm.Destroy()
		return nil, err
	}

	if gm.journal, err = platform.NewJournal(gm.dir); err != nil {
		gm.Destroy()
		return nil, err
	}

	if err := platform.StartMachine(gm, gm.journal); err != nil {
		gm.Destroy()
		return nil, err
	}

	gc.AddMach(gm)

	return gm, nil
}

func (gc *cluster) Destroy() {
	gc.BaseCluster.Destroy()
	gc.flight.DelCluster(gc)
}
