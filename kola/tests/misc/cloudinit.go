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

package misc

import (
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Run:         CloudInitBasic,
		ClusterSize: 1,
		Name:        "coreos.cloudinit.basic",
		UserData: conf.CloudConfig(`#cloud-config
hostname: "core1"
write_files:
  - path: "/foo"
    content: bar`),
		ExcludePlatforms: []string{"oci"},
	})
	register.Register(&register.Test{
		Run:         CloudInitScript,
		ClusterSize: 1,
		Name:        "coreos.cloudinit.script",
		UserData: conf.Script(`#!/bin/bash
echo bar > /foo
mkdir -p ~core/.ssh
cat <<EOF >> ~core/.ssh/authorized_keys
@SSH_KEYS@
EOF
chown -R core.core ~core/.ssh
chmod 700 ~core/.ssh
chmod 600 ~core/.ssh/authorized_keys`),
		ExcludePlatforms: []string{"oci"},
	})
}

func CloudInitBasic(c cluster.TestCluster) {
	m := c.Machines()[0]

	out := c.MustSSH(m, "cat /foo")
	if string(out) != "bar" {
		c.Fatalf("cloud-config produced unexpected value %q", out)
	}

	out = c.MustSSH(m, "hostnamectl")
	if !strings.Contains(string(out), "Static hostname: core1") {
		c.Fatalf("hostname wasn't set correctly:\n%s", out)
	}
}

func CloudInitScript(c cluster.TestCluster) {
	m := c.Machines()[0]

	out := c.MustSSH(m, "cat /foo")
	if string(out) != "bar" {
		c.Fatalf("userdata script produced unexpected value %q", out)
	}
}
