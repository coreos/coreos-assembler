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

package docker

import (
	"regexp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Run:         dockerTorcxFlagFile,
		ClusterSize: 1,
		Name:        "docker.torcx-flag-file",
		UserData: conf.ContainerLinuxConfig(`
storage:
  files:
    - filesystem: root
      path: /etc/coreos/docker-1.12
      contents:
        inline: yes
      mode: 0644
`),
	})
	register.Register(&register.Test{
		Run:         dockerTorcxFlagFileCloudConfig,
		ClusterSize: 1,
		Name:        "docker.torcx-flag-file.cloud-config",
		UserData: conf.CloudConfig(`
#cloud-config
write_files:
  - path: "/etc/coreos/docker-1.12"
    content: yes
`),
	})
}

func dockerTorcxFlagFile(c cluster.TestCluster) {
	m := c.Machines()[0]

	// flag=yes
	checkTorcxDockerVersions(c, m, `^1\.12$`, `^1\.12\.`)

	// flag=no
	c.MustSSH(m, "echo no | sudo tee /etc/coreos/docker-1.12")
	if err := m.Reboot(); err != nil {
		c.Fatalf("could not reboot: %v", err)
	}
	c.MustSSH(m, `sudo rm -rf /var/lib/docker`)
	checkTorcxDockerVersions(c, m, `^1[7-9]\.`, `^1[7-9]\.`)
}

func dockerTorcxFlagFileCloudConfig(c cluster.TestCluster) {
	m := c.Machines()[0]

	// cloudinit runs after torcx
	if err := m.Reboot(); err != nil {
		c.Fatalf("couldn't reboot: %v", err)
	}

	// flag=yes
	checkTorcxDockerVersions(c, m, `^1\.12$`, `^1\.12\.`)
}

func checkTorcxDockerVersions(c cluster.TestCluster, m platform.Machine, expectedRefRE, expectedVerRE string) {
	ref := getTorcxDockerReference(c, m)
	if !regexp.MustCompile(expectedRefRE).MatchString(ref) {
		c.Errorf("reference %s did not match %q", ref, expectedRefRE)
	}

	ver := getDockerServerVersion(c, m)
	if !regexp.MustCompile(expectedVerRE).MatchString(ver) {
		c.Errorf("version %s did not match %q", ver, expectedVerRE)
	}
}
