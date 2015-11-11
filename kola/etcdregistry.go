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

import "github.com/coreos/mantle/kola/tests/etcd"

//register new tests here
// "$name" and "$discovery" are substituted in the cloud config during cluster creation
func init() {
	// test etcd discovery with 0.4.7
	Register(&Test{
		Run:         etcd.DiscoveryV1,
		ClusterSize: 3,
		Name:        "coreos.etcd0.discovery",
		UserData: `#cloud-config
coreos:
  etcd:
    name: $name
    discovery: $discovery
    addr: $private_ipv4:2379
    peer-addr: $private_ipv4:2380`,
	})

	// test etcd discovery with 2.0 with new cloud config
	Register(&Test{
		Run:         etcd.DiscoveryV2,
		ClusterSize: 3,
		Name:        "coreos.etcd2.discovery",
		UserData: `#cloud-config

coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001`,
	})
}
