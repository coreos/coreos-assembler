// Copyright 2017 CoreOS, Inc.
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

package bootkube

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/pluton/spawn"
	"github.com/coreos/mantle/pluton/upstream"
)

func conformanceBootkube(c cluster.TestCluster) error {
	pc, err := spawn.MakeBootkubeCluster(c, 4, false)
	if err != nil {
		return err
	}

	return upstream.RunConformanceTests(pc)
}

func conformanceSelfEtcdBootkube(c cluster.TestCluster) error {
	pc, err := spawn.MakeBootkubeCluster(c, 4, true)
	if err != nil {
		return err
	}

	return upstream.RunConformanceTests(pc)
}
