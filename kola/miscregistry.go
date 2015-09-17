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
	Register(&Test{
		Run:         misc.NFSv3,
		ClusterSize: 0,
		Name:        "NFSv3",
		Platforms:   []string{"qemu", "aws"},
	})
	Register(&Test{
		Run:         misc.NFSv4,
		ClusterSize: 0,
		Name:        "NFSv4",
		Platforms:   []string{"qemu", "aws"},
	})
	Register(&Test{
		Run:         misc.NTP,
		ClusterSize: 0,
		Name:        "NTP",
		Platforms:   []string{"qemu"},
	})
}
