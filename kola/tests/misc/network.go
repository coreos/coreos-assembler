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

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
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

func checkListeners(m platform.Machine, protocol string, filter string, listeners []listener) error {
	var command string
	if filter != "" {
		command = fmt.Sprintf("sudo lsof -i%v -s%v", protocol, filter)
	} else {
		command = fmt.Sprintf("sudo lsof -i%v", protocol)
	}
	output, err := m.SSH(command)
	if err != nil {
		return fmt.Errorf("Failed to run %v: output %v, status: %v", command, output, err)
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
		portdata := strings.Split(data[8], ":")
		port := portdata[len(portdata)-1]
		for _, listener := range listeners {
			if processname == listener.process && port == listener.port {
				valid = true
			}
		}
		if valid != true {
			return fmt.Errorf("Unexpected listening process: %v on %v", processname, port)
		}
	}
	return nil
}

func NetworkListeners(c platform.TestCluster) error {
	m := c.Machines()[0]

	TCPListeners := []listener{
		{"systemd", "ssh"},
	}
	UDPListeners := []listener{
		{"systemd-n", "dhcpv6-client"},
		{"systemd-n", "bootpc"},
	}
	err := checkListeners(m, "TCP", "TCP:LISTEN", TCPListeners)
	if err != nil {
		return err
	}
	err = checkListeners(m, "UDP", "", UDPListeners)
	if err != nil {
		return err
	}

	return nil
}
