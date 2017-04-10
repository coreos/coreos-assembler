// Copyright 2016 CoreOS, Inc.
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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

func init() {
	register.Register(&register.Test{
		Run:         NetworkListeners,
		ClusterSize: 1,
		Name:        "coreos.network.listeners",
		UserData:    `#cloud-config`,
	})
}

type listener struct {
	process string
	port    string
}

func checkListeners(c cluster.TestCluster, protocol string, filter string, listeners []listener) {
	m := c.Machines()[0]

	var command string
	if filter != "" {
		command = fmt.Sprintf("sudo lsof -i%v -s%v", protocol, filter)
	} else {
		command = fmt.Sprintf("sudo lsof -i%v", protocol)
	}
	output, err := m.SSH(command)
	if err != nil {
		c.Fatalf("Failed to run %s: output %s, status: %v", command, output, err)
	}

	processes := strings.Split(string(output), "\n")

	for i, process := range processes {
		var valid bool
		// skip header
		if i == 0 {
			continue
		}
		data := strings.Fields(process)
		processname := data[0]
		pid := data[1]
		portdata := strings.Split(data[8], ":")
		port := portdata[len(portdata)-1]
		for _, listener := range listeners {
			if processname == listener.process && port == listener.port {
				valid = true
			}
		}
		if valid != true {
			// systemd renames child processes in parentheses before closing their fds
			if processname[0] == '(' {
				c.Logf("Ignoring %q listener process: %q (pid %s) on %q", protocol, processname, pid, port)
			} else {
				c.Fatalf("Unexpected %q listener process: %q (pid %s) on %q", protocol, processname, pid, port)
			}
		}
	}
}

func NetworkListeners(c cluster.TestCluster) {
	TCPListeners := []listener{
		{"systemd", "ssh"},
	}
	UDPListeners := []listener{
		{"systemd-n", "dhcpv6-client"},
		{"systemd-n", "bootpc"},
	}
	checkListeners(c, "TCP", "TCP:LISTEN", TCPListeners)
	checkListeners(c, "UDP", "", UDPListeners)
}
