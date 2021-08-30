// Copyright 2020 Red Hat, Inc.
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
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         verifySSSD,
		ClusterSize: 1,
		Name:        `rhcos.sssd`,
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu"},
		UserData: conf.Ignition(`{
			"ignition": {
				"version": "3.0.0"
			}
		}`),
	})
}

// Verify changes made in https://gitlab.cee.redhat.com/coreos/redhat-coreos/commit/03ba15aa429af7bf07531e0a533d691dbfd623c4
// do not cause regression by checking /etc/nsswitch.conf contains 'altfiles'.
// For example, calling `sudo authselect select --force sssd with-custom-passwd with-custom-group` would
// remove 'altfiles' in /etc/nsswitch.conf, which will fail sssd.service.
func verifyNssAltfiles(c cluster.TestCluster, m platform.Machine) {
	// MustSSH will panic if exit status is non-zero
	// Use -q option to indicate that we only care about the exit status
	c.RunCmdSync(m, "grep -q altfiles /etc/nsswitch.conf")
}

func verifyPamConfigs(c cluster.TestCluster, m platform.Machine) {
	// MustSSH will panic if exit status is non-zero
	// Use -q option to indicate that we only care about the exit status
	c.RunCmdSync(m, "grep -q pam_sss.so /etc/pam.d/password-auth")
	c.RunCmdSync(m, "grep -q pam_sss.so /etc/pam.d/system-auth")
}

func verifySSSD(c cluster.TestCluster) {
	m := c.Machines()[0]

	verifyNssAltfiles(c, m)
	verifyPamConfigs(c, m)
}
