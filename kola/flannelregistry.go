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

import (
	"bytes"
	"text/template"

	"github.com/coreos/mantle/kola/tests/flannel"
)

var (
	flannelConf = template.Must(template.New("flannel-userdata").Parse(`#cloud-config
coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001
  units:
    - name: etcd2.service
      command: start
    - name: flanneld.service
      drop-ins:
        - name: 50-network-config.conf
          content: |
            [Service]
            ExecStartPre=/usr/bin/etcdctl set /coreos.com/network/config '{ "Network":"10.254.0.0/16", "Backend":{"Type": "{{.}}"} }'
      command: start
    - name: docker.service
      command: start
`))
)

//register new tests here
// "$name" and "$discovery" are substituted in the cloud config during cluster creation
func init() {
	udpConf := new(bytes.Buffer)
	if err := flannelConf.Execute(udpConf, "udp"); err != nil {
		panic(err)
	}

	Register(&Test{
		Run:         flannel.UDP,
		ClusterSize: 3,
		Name:        "FlannelUDP",
		Platforms:   []string{"aws", "gce"},
		UserData:    udpConf.String(),
	})

	vxlanConf := new(bytes.Buffer)
	if err := flannelConf.Execute(vxlanConf, "vxlan"); err != nil {
		panic(err)
	}

	Register(&Test{
		Run:         flannel.VXLAN,
		ClusterSize: 3,
		Name:        "FlannelVXLAN",
		Platforms:   []string{"aws", "gce"},
		UserData:    vxlanConf.String(),
	})
}
