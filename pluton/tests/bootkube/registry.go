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
	"github.com/coreos/mantle/pluton"
	"github.com/coreos/mantle/pluton/harness"
)

func init() {
	// main test suite run on every PR
	harness.Register(pluton.Test{
		Name: "bootkube.smoke",
		Run:  smoke,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})
	harness.Register(pluton.Test{
		Name: "bootkube.destruct.reboot",
		Run:  rebootMaster,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	harness.Register(pluton.Test{
		Name: "bootkube.destruct.delete",
		Run:  deleteAPIServer,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	// main self-hosted test suite run on every PR
	harness.Register(pluton.Test{
		Name: "bootkube.selfetcd.smoke",
		Run:  smoke,
		Options: pluton.Options{
			SelfHostEtcd:   true,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	harness.Register(pluton.Test{
		Name: "bootkube.selfetcd.scale",
		Run:  etcdScale,
		Options: pluton.Options{
			SelfHostEtcd:   true,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	harness.Register(pluton.Test{
		Name: "bootkube.selfetcd.destruct.reboot",
		Run:  rebootMaster,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	harness.Register(pluton.Test{
		Name: "bootkube.selfetcd.destruct.delete",
		Run:  deleteAPIServer,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 1,
		},
	})

	// conformance
	harness.Register(pluton.Test{
		Name: "conformance.bootkube",
		Run:  conformanceBootkube,
		Options: pluton.Options{
			SelfHostEtcd:   false,
			InitialMasters: 1,
			InitialWorkers: 4,
		},
	})
	harness.Register(pluton.Test{
		Name: "conformance.selfetcd.bootkube",
		Run:  conformanceBootkube,
		Options: pluton.Options{
			SelfHostEtcd:   true,
			InitialMasters: 1,
			InitialWorkers: 4,
		},
	})

}
