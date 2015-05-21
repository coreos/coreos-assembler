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

import "github.com/coreos/mantle/kola/tests/misc"

//register new tests here
// "$name" and "$discovery" are substituted in the cloud config during cluster creation
func init() {
	// test etcd discovery with 0.4.7
	Register(&Test{
		Run:         misc.NFS,
		ClusterSize: 0,
		Name:        "NFS",
		Platforms:   []string{"qemu"},
	})
}
