// Copyright 2018 Red Hat, Inc.
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

package crio

import (
	"encoding/json"
	"fmt"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

// simplifiedCrioInfo represents the results from crio info
type simplifiedCrioInfo struct {
	StorageDriver string `json:"storage_driver"`
	StorageRoot   string `json:"storage_root"`
	CgroupDriver  string `json:"cgroup_driver"`
}

// RHCOS has the crio service disabled by default. In OCP, it's activated via
// kubelet.service, which `Requires` it. Let's model that too here by having
// a mock kubelet.service that pulls it in. This also makes it work with
// `--oscontainer`.
var enableCrioIgn = conf.Ignition(`{
  "ignition": {
    "version": "3.0.0"
  },
  "systemd": {
    "units": [
      {
        "enabled": true,
        "name": "kubelet.service",
        "contents": "[Unit]\nRequires=crio.service\n[Service]\nType=oneshot\nExecStart=true\nRemainAfterExit=yes\n[Install]\nWantedBy=multi-user.target"
      }
    ]
  }
}`)

// init runs when the package is imported and takes care of registering tests
func init() {
	register.RegisterTest(&register.Test{
		Run:         crioBaseTests,
		ClusterSize: 1,
		Name:        `crio.base`,
		Description: "Verify cri-o basic funcions work, include storage driver is overlay, storage root is /varlib/containers/storage, cgroup driver is systemd",
		Distros:     []string{"rhcos"},
		UserData:    enableCrioIgn,
		// crio pods require fetching a kubernetes pause image
		Tags:        []string{"crio", kola.NeedsInternetTag},
		RequiredTag: "openshift",
	})
}

// crioBaseTests executes multiple tests under the "base" name
func crioBaseTests(c cluster.TestCluster) {
	c.Run("crio-info", testCrioInfo)
}

// getCrioInfo parses and returns the information crio provides via socket
func getCrioInfo(c cluster.TestCluster, m platform.Machine) (simplifiedCrioInfo, error) {
	target := simplifiedCrioInfo{}
	crioInfoJSON, err := c.SSH(m, `sudo curl -s --unix-socket /var/run/crio/crio.sock http://crio/info`)
	if err != nil {
		return target, fmt.Errorf("could not get info: %v", err)
	}

	err = json.Unmarshal(crioInfoJSON, &target)
	if err != nil {
		return target, fmt.Errorf("could not unmarshal info %q into known json: %v", string(crioInfoJSON), err)
	}
	return target, nil
}

// testCrioInfo test that crio info's output is as expected.
func testCrioInfo(c cluster.TestCluster) {
	m := c.Machines()[0]
	info, err := getCrioInfo(c, m)
	if err != nil {
		c.Fatal(err)
	}
	expectedStorageDriver := "overlay"
	if info.StorageDriver != expectedStorageDriver {
		c.Errorf("unexpected storage driver: %v != %v", expectedStorageDriver, info.StorageDriver)
	}
	expectedStorageRoot := "/var/lib/containers/storage"
	if info.StorageRoot != expectedStorageRoot {
		c.Errorf("unexpected storage root: %v != %v", expectedStorageRoot, info.StorageRoot)
	}
	expectedCgroupDriver := "systemd"
	if info.CgroupDriver != expectedCgroupDriver {
		c.Errorf("unexpected cgroup driver: %v != %v", expectedCgroupDriver, info.CgroupDriver)
	}

}
