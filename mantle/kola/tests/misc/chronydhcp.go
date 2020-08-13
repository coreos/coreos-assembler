// Copyright 2020 Red Hat
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
	"fmt"
	"strings"
	"time"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
	"github.com/vincent-petithory/dataurl"
)

var script = []byte(`#!/bin/bash
# This script creates two veth interfaces i.e. one for the host machine 
# and other for the container(dnsmasq server). This setup will be helpful
# to verify the DHCP propagation of NTP servers
# https://github.com/coreos/fedora-coreos-config/pull/412

set -euo pipefail

# This is needed to run a container with systemd
setsebool container_manage_cgroup 1

# create a network namespace
ip netns add container

# create veth pair and assign a namespace to veth-container
ip link add veth-host type veth peer name veth-container
ip link set veth-container netns container

# assign an IP address to the 'veth-container' interface and bring it up
ip netns exec container ip address add 172.16.0.1/24 dev veth-container
ip netns exec container ip link set veth-container up

# create a static ethernet connection for the 'veth-host'
nmcli dev set veth-host managed yes
ip link set veth-host up

# run podman commands to set up dnsmasq server
pushd $(mktemp -d)
NTPHOSTIP=$(getent hosts time-c-g.nist.gov | cut -d ' ' -f 1)
cat <<EOF >Dockerfile
FROM registry.fedoraproject.org/fedora:32
RUN dnf -y install systemd dnsmasq iproute iputils
RUN dnf clean all
RUN systemctl enable dnsmasq
RUN echo -e 'dhcp-range=172.16.0.10,172.16.0.20,12h\nbind-interfaces\ninterface=veth-container\ndhcp-option=option:ntp-server,$NTPHOSTIP' > /etc/dnsmasq.d/dhcp
CMD [ "/sbin/init" ]
EOF
podman build -t dnsmasq .
popd
podman run -d --rm --name dnsmasq --privileged --network ns:/var/run/netns/container dnsmasq`)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.chrony.dhcp.verify",
		Run:         verifyDHCPPropagationOfNTPServers,
		ClusterSize: 1,
		Tags:        []string{"chrony", "dhcp", "ntp", kola.NeedsInternetTag},
		UserDataV3: conf.Ignition(fmt.Sprintf(`{
			"ignition": {"version": "3.0.0"},
			"storage": {
				"files": [
				  {
					"group": {},
					"path": "/usr/local/bin/setup.sh",
					"user": {},
					"contents": {
					  "source": %q,
					  "verification": {}
					},
					"mode": 493
				  }
				]
			  },
			  "systemd": {
				"units": [
				  {
					"contents": "[Unit]\nDescription=verify chrony using NTP settings from DHCP\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/setup.sh\nRestart=on-failure\nRemainAfterExit=yes\n[Install]\nWantedBy=multi-user.target\n",
					"enabled": true,
					"name": "chrony-dhcp.service"
				  }
				]
			  }
			}`, dataurl.EncodeBytes(script))),
	})
}

func verifyDHCPPropagationOfNTPServers(c cluster.TestCluster) {
	m := c.Machines()[0]
	// Wait a little bit for the chrony-dhcp.service to start
	if err := util.WaitUntilReady(10*time.Minute, 5*time.Second, func() (bool, error) {
		_, _, err := m.SSH("sudo systemctl status chrony-dhcp.service")
		if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		c.Fatal("failed to start chrony-dhcp.service: %v", err)
	}

	out := c.MustSSH(m, "chronyc sources")
	// Name: time-c-g.nist.gov  IP address: 129.6.15.30
	if !strings.Contains(string(out), "time-c-g.nist.gov") {
		c.Fatalf("propagation of ntp server information via dhcp failed")
	}
}
