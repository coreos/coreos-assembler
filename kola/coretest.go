// Copyright 2015 CoreOS, Inc.
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

package kola

import "github.com/coreos/mantle/kola/tests/coretest"

func init() {
	Register(&Test{
		Name:        "coretestsLocal",
		Run:         coretest.LocalTests,
		ClusterSize: 1,
		NativeFuncs: map[string]func() error{
			"CloudConfig":      coretest.TestCloudinitCloudConfig,
			"Script":           coretest.TestCloudinitScript,
			"PortSSH":          coretest.TestPortSsh,
			"DbusPerms":        coretest.TestDbusPerms,
			"Symlink":          coretest.TestSymlinkResolvConf,
			"UpdateEngineKeys": coretest.TestInstalledUpdateEngineRsaKeys,
			"ServicesActive":   coretest.TestServicesActive,
			"ReadOnly":         coretest.TestReadOnlyFs,
			"RandomUUID":       coretest.TestFsRandomUUID,
			"Useradd":          coretest.TestUseradd,
		},
	})
	Register(&Test{
		Name:        "coretestsCluster",
		Run:         coretest.ClusterTests,
		ClusterSize: 3,
		NativeFuncs: map[string]func() error{
			"EtcdUpdateValue":    coretest.TestEtcdUpdateValue,
			"FleetctlRunService": coretest.TestFleetctlRunService,
		},
		UserData: `#cloud-config

coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001
  fleet:
    etcd-request-timeout: 15 
  units:
    - name: etcd2.service
      command: start
    - name: fleet.service
      command: start`,
	})

	// tests requiring network connection to internet
	Register(&Test{
		Name:        "coretestsInternetLocal",
		Run:         coretest.InternetTests,
		ClusterSize: 1,
		Platforms:   []string{"gce", "aws"},
		NativeFuncs: map[string]func() error{
			"UpdateEngine": coretest.TestUpdateEngine,
			"DockerPing":   coretest.TestDockerPing,
			"DockerEcho":   coretest.TestDockerEcho,
			"NTPDate":      coretest.TestNTPDate,
		},
	})
}
