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

package kubernetes

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

const hyperkubeTag = "v1.5.7_coreos.0"
const versionOutput = "Kubernetes v1.5.7+coreos.0" // --version for /hyperkube

func init() {
	// regression test for https://github.com/coreos/bugs/issues/1892
	register.Register(&register.Test{
		Name:        "kubernetes.kubelet_wrapper.var-log-mount",
		Run:         testKubeletWrapperVarLog,
		ClusterSize: 1,
		UserData: conf.ContainerLinuxConfig(`
systemd:
  units:
  - name: kubelet.service
    enable: true
    contents: |-
      [Service]
      Type=oneshot
      Environment=KUBELET_VERSION=` + hyperkubeTag + `
      # var-log and resolv were at various times either in the kubelet-wrapper
      # docs or recommended to people
      Environment="RKT_OPTS=--volume var-log,kind=host,source=/var/log \
        --mount volume=var-log,target=/var/log \
        --volume resolv,kind=host,source=/etc/resolv.conf \
        --mount volume=resolv,target=/etc/resolv.conf"

      # The regression was in rkt's handling of RKT_OPTS; if we get far enough
      # that rkt runs the kubelet successfully, we haven't hit this regression,
      # so just printing the version is enough.
      ExecStart=/usr/lib/coreos/kubelet-wrapper --version
      [Install]
      WantedBy=multi-user.target
`),
		ExcludePlatforms: []string{"qemu"}, // network access for hyperkube
	})
}

func testKubeletWrapperVarLog(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Wait up to 10 minutes the version
	_, err := c.SSH(m, `
	for i in {1..120}; do 
		sleep 5
		if journalctl -u kubelet -o cat | grep '`+versionOutput+`' &>/dev/null; then
			exit 0
		fi
	done
	journalctl -u kubelet -o cat
	exit 1`)
	if err != nil {
		c.Fatalf("unable to get expected kubelet.service version output: %v", err)
	}
}
