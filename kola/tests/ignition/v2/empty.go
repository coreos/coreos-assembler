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

package ignition

import (
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/go-semver/semver"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2.empty",
		Run:         empty,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		MinVersion:  semver.Version{Major: 1010},
	})
}

func empty(_ platform.TestCluster) error {
	return nil
}
