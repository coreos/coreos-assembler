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
	Test{
		Run:         etcdDiscovery,
		Discovery:   true,
		ClusterSize: 3,
		Name:        "etcdDiscovery",
		CloudConfig: `#cloud-config

coreos:
  etcd:
      name: $name
      discovery: $discovery
      addr: $public_ipv4:4001
      peer-addr: $private_ipv4:7001`,
	},
}

type Test struct {
	Run         func(platform.Cluster) error
	Discovery   bool // set true to setup discovery endpoint
	ClusterSize int
	Name        string
	CloudConfig string
}
