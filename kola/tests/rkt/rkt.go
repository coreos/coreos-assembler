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

package rkt

import (
	"fmt"
	"time"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var conf = `{
	"ignition": {
		"version": "2.0.0"
	},
	"systemd": {
		"units": [{
			"name": "etcd3-wrapper.service",
			"enable": true,
		}]
	}
}`

func init() {
	register.Register(&register.Test{
		Run:         rktEtcd,
		ClusterSize: 1,
		Platforms:   []string{"aws", "gce"},
		Name:        "coreos.rkt.etcd3",
		UserData:    conf,
		MinVersion:  semver.Version{Major: 1106},
	})
}

func rktEtcd(t platform.TestCluster) error {
	m := t.Machines()[0]

	etcdCmd := "etcdctl cluster-health"
	etcdCheck := func() error {
		output, err := m.SSH(etcdCmd)
		if err != nil {
			return fmt.Errorf("failed to run %q: output: %q status: %q", etcdCmd, output, err)
		}

		return nil
	}

	if err := util.Retry(60, 3*time.Second, etcdCheck); err != nil {
		t.Errorf("etcd in rkt failed health check: %v", err)
	}

	return nil
}
