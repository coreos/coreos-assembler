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

import "github.com/coreos/mantle/kola/tests/ignition"

func init() {
	Register(&Test{
		Name:        "coreos.ignition.sethostname",
		Run:         ignition.SetHostname,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		UserData: `{
        "ignitionVersion": 1,
        "storage": {
                "filesystems": [
                        {
                                "device": "/dev/disk/by-partlabel/ROOT",
                                "format": "ext4",
                                "files": [
                                        {
                                                "path": "/etc/hostname",
                                                "mode": 420,
                                                "contents": "core1"
                                        }
                                ]
                        }
                ]
        }
}
`,
	})
}
