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
	"fmt"
	"net"
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
)

var (
	flannelConf = `{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd2.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd2 --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379,http://0.0.0.0:4001 --listen-peer-urls=http://$private_ipv4:2380,http://$private_ipv4:7001"
        }]
      },
      {
        "name": "flanneld.service",
        "enable": true,
        "dropins": [{
          "name": "50-network-config.conf",
          "contents": "[Service]\nExecStartPre=/usr/bin/etcdctl set /coreos.com/network/config '{ \"Network\": \"10.254.0.0/16\", \"Backend\": {\"Type\": \"$type\"} }'"
        }]
      },
      {
        "name": "flannel-docker-opts.service",
        "dropins": [{
          "name": "retry.conf",
          "contents": "[Service]\nTimeoutStartSec=300\nExecStart=\nExecStart=/bin/sh -exc 'for try in 1 2 3 4 5 6 ; do /usr/lib/coreos/flannel-wrapper -d /run/flannel/flannel_docker_opts.env -i && break || sleep 10 ; try=fail ; done ; [ $try != fail ]'"
        }]
      },
      {
        "name": "docker.service",
        "enable": true
      },
      {
        "name": "coreos-metadata.service",
        "dropins": [{
          "name": "qemu.conf",
          "contents": "[Unit]\nConditionKernelCommandLine=coreos.oem.id"
        }]
      }
    ]
  }
}`
)

func init() {
	register.Register(&register.Test{
		Run:         udp,
		ClusterSize: 3,
		Name:        "coreos.flannel.udp",
		Platforms:   []string{"aws", "gce"},
		UserData:    strings.Replace(flannelConf, "$type", "udp", -1),
	})

	register.Register(&register.Test{
		Run:         vxlan,
		ClusterSize: 3,
		Name:        "coreos.flannel.vxlan",
		Platforms:   []string{"aws", "gce"},
		UserData:    strings.Replace(flannelConf, "$type", "vxlan", -1),
	})
}

// get docker bridge ip from a machine
func mach2bip(m platform.Machine, ifname string) (string, error) {
	// note the escaped % in awk.
	out, err := m.SSH(fmt.Sprintf(`/usr/lib/systemd/systemd-networkd-wait-online --interface=%s --timeout=60 ; ip -4 -o addr show dev %s primary | awk -F " +|/" '{printf "%%s", $4}'`, ifname, ifname))
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
func ping(c cluster.TestCluster, a, b platform.Machine, ifname string) {
	srcip, err := mach2bip(a, ifname)
	if err != nil {
		c.Fatalf("failed to get docker bridge ip #1: %v", err)
	}

	dstip, err := mach2bip(b, ifname)
	if err != nil {
		c.Fatalf("failed to get docker bridge ip #2: %v", err)
	}

	// ensure the docker bridges have the right network
	_, ipnet, _ := net.ParseCIDR("10.254.0.0/16")
	if !ipnet.Contains(net.ParseIP(srcip)) || !ipnet.Contains(net.ParseIP(dstip)) {
		c.Fatalf("bridge ips (%s %s) not in flannel network (%s)", srcip, dstip, ipnet)
	}

	c.Logf("ping from %s(%s) to %s(%s)", a.ID(), srcip, b.ID(), dstip)

	cmd := fmt.Sprintf("ping -c 10 -I %s %s", srcip, dstip)
	out, err := a.SSH(cmd)
	if err != nil {
		c.Fatalf("ping from %s to %s failed: %s: %v", a.ID(), b.ID(), out, err)
	}
}

// UDP tests that flannel can send packets using the udp backend.
func udp(c cluster.TestCluster) {
	machs := c.Machines()

	// Wait for all etcd cluster nodes to be ready.
	if err := etcd.GetClusterHealth(machs[0], len(machs)); err != nil {
		c.Fatalf("cluster health: %v", err)
	}

	ping(c, machs[0], machs[2], "flannel0")
}

// VXLAN tests that flannel can send packets using the vxlan backend.
func vxlan(c cluster.TestCluster) {
	machs := c.Machines()

	// Wait for all etcd cluster nodes to be ready.
	if err := etcd.GetClusterHealth(machs[0], len(machs)); err != nil {
		c.Fatalf("cluster health: %v", err)
	}

	ping(c, machs[0], machs[2], "flannel.1")
}
