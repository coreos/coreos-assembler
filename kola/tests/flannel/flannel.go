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

// flannel tests. tests assume flannel is using the 10.254.0.0/16 network.
// these tests should really assert no units failed during boot (such as flanneld)
// it is also unfortunate that we must retry, but starting
// early-docker -> flanneld -> docker ->docker0 may not be ready by the time we ssh in.
package flannel

import (
	"bytes"
	"fmt"
	"net"
	"text/template"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var (
	plog        = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/flannel")
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

func init() {
	udpConf := new(bytes.Buffer)
	if err := flannelConf.Execute(udpConf, "udp"); err != nil {
		panic(err)
	}

	register.Register(&register.Test{
		Run:         udp,
		ClusterSize: 3,
		Name:        "coreos.flannel.udp",
		Platforms:   []string{"aws", "gce"},
		UserData:    udpConf.String(),
	})

	vxlanConf := new(bytes.Buffer)
	if err := flannelConf.Execute(vxlanConf, "vxlan"); err != nil {
		panic(err)
	}

	register.Register(&register.Test{
		Run:         vxlan,
		ClusterSize: 3,
		Name:        "coreos.flannel.vxlan",
		Platforms:   []string{"aws", "gce"},
		UserData:    vxlanConf.String(),
	})
}

// get docker bridge ip from a machine
func mach2bip(m platform.Machine, ifname string) (string, error) {
	// note the escaped % in awk.
	out, err := m.SSH(fmt.Sprintf(`ip -4 -o addr show dev %s primary | awk -F " +|/" '{printf "%%s", $4}'`, ifname))
	if err != nil {
		return "", err
	}

	// XXX(mischief): unfortunately `ip` does not return a nonzero status if the interface doesn't exist.
	if len(out) == 0 {
		return "", fmt.Errorf("interface %q doesn't exist?", ifname)
	}

	return string(out), nil
}

// ping sends icmp packets from machine a to b using the ping tool.
func ping(a, b platform.Machine, ifname string) error {
	srcip, err := mach2bip(a, ifname)
	if err != nil {
		return fmt.Errorf("failed to get docker bridge ip: %v", err)
	}

	dstip, err := mach2bip(b, ifname)
	if err != nil {
		return fmt.Errorf("failed to get docker bridge ip: %v", err)
	}

	// ensure the docker bridges have the right network
	_, ipnet, _ := net.ParseCIDR("10.254.0.0/16")
	if !ipnet.Contains(net.ParseIP(srcip)) || !ipnet.Contains(net.ParseIP(dstip)) {
		return fmt.Errorf("bridge ips (%s %s) not in flannel network (%s)", srcip, dstip, ipnet)
	}

	plog.Infof("ping from %s(%s) to %s(%s)", a.ID(), srcip, b.ID(), dstip)

	cmd := fmt.Sprintf("ping -c 10 -I %s %s", srcip, dstip)
	out, err := a.SSH(cmd)
	if err != nil {
		return fmt.Errorf("ping from %s to %s failed: %s: %v", a.ID(), b.ID(), out, err)
	}

	return nil
}

// UDP tests that flannel can send packets using the udp backend.
func udp(c cluster.TestCluster) error {
	machs := c.Machines()
	return util.Retry(12, 15*time.Second, func() error { return ping(machs[0], machs[2], "flannel0") })
}

// VXLAN tests that flannel can send packets using the vxlan backend.
func vxlan(c cluster.TestCluster) error {
	machs := c.Machines()
	return util.Retry(12, 15*time.Second, func() error { return ping(machs[0], machs[2], "flannel.1") })
}
