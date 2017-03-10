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
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/register"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "pluton/tests/bootkube")

func init() {
	register.Register(&register.Test{
		Name:      "bootkube.smoke",
		Run:       bootkubeSmoke,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.selfetcd.smoke",
		Run:       bootkubeSmokeEtcd,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.selfetcd.scale",
		Run:       etcdScale,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.destruct.reboot",
		Run:       rebootMaster,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.destruct.delete",
		Run:       deleteAPIServer,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "conformance.bootkube",
		Run:       conformanceBootkube,
		Platforms: []string{"gce"},
	})
	register.Register(&register.Test{
		Name:      "conformance.selfetcd.bootkube",
		Run:       conformanceSelfEtcdBootkube,
		Platforms: []string{"gce"},
	})

}
