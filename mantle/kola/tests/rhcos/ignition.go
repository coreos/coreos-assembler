// Copyright 2018 Red Hat
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

package rhcos

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "rhcos.ignition.misc",
		Run:         verifyIgnitionMisc,
		ClusterSize: 1,
		Tags:        []string{"ignition"},
		Flags:       []register.Flag{register.RequiresInternetAccess}, // fetching ignition from github requires networking
		Distros:     []string{"rhcos"},
		// qemu-unpriv machines cannot communicate to network
		ExcludePlatforms: []string{"qemu-unpriv"},
		// This is to verify (https://bugzilla.redhat.com/show_bug.cgi?id=1980679) for RHCOS
		// remote kargs_file.ign on github: inject kernelArguments and write something to /etc/testfile
		// config file to include remote kargsfile.ign
		UserData: conf.Ignition(`{
			"ignition": {
				"version": "3.3.0",
				"config": {
					"merge": [
						{
							"source": "https://raw.githubusercontent.com/HuijingHei/coreos-assembler/hhei-dev/mantle/kola/tests/rhcos/kargsfile.ign",
							"verification": {
								"hash": "sha512-1f33f03dff87207ec2e5686c8fe001f7d5c7181315aaed9056ed4509bf53c11fbe97c71c679b6648615914f1bb5d86d86e65b69a2469df36215b76812fe58d52"
							}
						}
					]
				}
			}
		}`),
	})
}

func verifyIgnitionMisc(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Verify kernel arguments and /etc/testfile
	c.RunCmdSync(m, "grep -q foobar /proc/cmdline")
	c.RunCmdSync(m, "test -e /etc/testfile")
}
