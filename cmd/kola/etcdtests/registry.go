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

package etcdtests

import "github.com/coreos/mantle/platform"

//register new tests here
// "$name" and "$discovery" are substituted in the cloud config during cluster creation
var Tests = []Test{
	// test etcd discovery with 0.4.7
	Test{
		Run:         etcdDiscovery,
		Discovery:   true,
		ClusterSize: 3,
		Name:        "etcdDiscovery--version1",
		CloudConfig: `#cloud-config
write_files:
  - path: /run/systemd/system/etcd.service.d/30-exec.conf
    permissions: 0644
    content: |
      [Service]
      ExecStart=
      ExecStart=/usr/libexec/etcd/internal_versions/1

coreos:
  etcd:
    name: $name
    discovery: $discovery
    addr: $public_ipv4:4001
    peer-addr: $private_ipv4:7001`,
	},

	// test etcd discovery with 2.0 with new cloud config
	Test{
		Run:         etcdDiscovery,
		Discovery:   true,
		ClusterSize: 3,
		Name:        "etcdDiscovery--version2--oldconfig",
		CloudConfig: `#cloud-config
write_files:
  - path: /run/systemd/system/etcd.service.d/30-exec.conf
    permissions: 0644
    content: |
      [Service]
      ExecStart=
      ExecStart=/usr/libexec/etcd/internal_versions/2

coreos:
  etcd:
    name: $name
    discovery: $discovery
    addr: $public_ipv4:4001
    peer-addr: $private_ipv4:7001`,
	},

	// test etcd discovery with 2.0 but with old cloud config
	Test{
		Run:         etcdDiscovery,
		Discovery:   true,
		ClusterSize: 3,
		Name:        "etcdDiscovery--version2",
		CloudConfig: `#cloud-config
write_files:
  - path: /run/systemd/system/etcd.service.d/30-exec.conf
    permissions: 0644
    content: |
      [Service]
      ExecStart=
      ExecStart=/usr/libexec/etcd/internal_versions/2
coreos:
  etcd:
    name: $name
    discovery: $discovery
    listen-client-urls: http://0.0.0.0:4001,http://0.0.0.0:2379
    advertise-client-urls: http://0.0.0.0:4001,http://0.0.0.0:2379
    initial-advertise-peer-urls: http://$public_ipv4:2380
    listen-peer-urls: http://$private_ipv4:2380`,
	},
}

type Test struct {
	Run         func(platform.Cluster) error
	Discovery   bool // set true to setup discovery endpoint
	ClusterSize int
	Name        string
	CloudConfig string
}
