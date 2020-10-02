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
	"regexp"
	"strings"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         NetworkListeners,
		ClusterSize: 1,
		Name:        "fcos.network.listeners",
		Distros:     []string{"fcos"},
		// be sure to notice listeners in the docker stack
		UserData: conf.ContainerLinuxConfig(`systemd:
  units:
    - name: docker.service
      enabled: true`),
	})
	// TODO: rewrite test for NetworkManager
	register.RegisterTest(&register.Test{
		Run:              NetworkInitramfsSecondBoot,
		ClusterSize:      1,
		Name:             "coreos.network.initramfs.second-boot",
		ExcludePlatforms: []string{"do"},
		ExcludeDistros:   []string{"fcos", "rhcos"},
	})
}

type listener struct {
	// udp or tcp; note each v4 variant will also match 'v6'
	protocol string
	port     string
	process  string
}

func checkListeners(c cluster.TestCluster, expectedListeners []listener) error {
	m := c.Machines()[0]

	output := c.MustSSH(m, "sudo ss -plutn")

	processes := strings.Split(string(output), "\n")
	// verify header is as expected
	if len(processes) < 1 {
		c.Fatalf("expected at least one line of ss output: %q", output)
	}
	// ss output's header sometimes does not have whitespace between "Peer Address:Port" and "Process"
	headerRegex := `Netid\s+State\s+Recv-Q\s+Send-Q\s+Local Address:Port\s+Peer Address:Port\s*Process`
	if !regexp.MustCompile(headerRegex).MatchString(processes[0]) {
		c.Fatalf("ss output has changed format: %q", processes[0])
	}
	// skip header
	processes = processes[1:]

	// create expectedListeners map
	expectedListenersMap := map[listener]bool{}
	for _, expected := range expectedListeners {
		expectedListenersMap[expected] = true
	}

NextProcess:
	/*
		Sample `sudo ss -plutn` output:
		Netid  State   Recv-Q  Send-Q  Local Address:Port  Peer Address:Port   Process
		udp    UNCONN  0       0           127.0.0.1:323        0.0.0.0:*      users:(("chronyd",pid=856,fd=5))
		udp    UNCONN  0       0               [::1]:323           [::]:*      users:(("chronyd",pid=856,fd=6))
		tcp    LISTEN  0       128           0.0.0.0:22         0.0.0.0:*      users:(("sshd",pid=1156,fd=5))
		tcp    LISTEN  0       128              [::]:22            [::]:*      users:(("sshd",pid=1156,fd=7))
	*/
	for _, line := range processes {
		parts := strings.Fields(line)
		if len(parts) != 7 {
			c.Fatalf("unexpected number of parts on line: %q in output %q", line, output)
		}
		proto := parts[0]
		portData := strings.Split(parts[4], ":")
		port := portData[len(portData)-1]
		processData := parts[len(parts)-1]
		processStr := regexp.MustCompile(`".+"`).FindString(processData) // process name is captured inside double quotes
		if processStr == "" {
			c.Errorf("%v did not contain program; full output: %q", processData, output)
			continue
		}
		process := processStr[1 : len(processStr)-1]
		thisListener := listener{
			process:  process,
			protocol: proto,
			port:     port,
		}

		if expectedListenersMap[thisListener] {
			// matches expected process
			continue NextProcess
		}

		c.Logf("full ss output: %q", output)
		return fmt.Errorf("Unexpected listener process: %q", line)
	}

	return nil
}

// NetworkListeners checks for listeners with ss.
func NetworkListeners(c cluster.TestCluster) {
	expectedListeners := []listener{
		{"tcp", "22", "sshd"},
		{"udp", "323", "chronyd"},
		// DNS via systemd-resolved
		{"tcp", "53", "systemd-resolve"},
		{"udp", "53", "systemd-resolve"},
		// systemd-resolved also listens on 5355 for Link-Local Multicast Name Resolution
		// https://serverfault.com/a/929642
		{"tcp", "5355", "systemd-resolve"},
		{"udp", "5355", "systemd-resolve"},
	}
	checkList := func() error {
		return checkListeners(c, expectedListeners)
	}
	if err := util.Retry(3, 5*time.Second, checkList); err != nil {
		c.Errorf(err.Error())
	}
}

// NetworkInitramfsSecondBoot verifies that networking is not started in the initramfs on the second boot.
// https://github.com/coreos/bugs/issues/1768
func NetworkInitramfsSecondBoot(c cluster.TestCluster) {
	m := c.Machines()[0]

	m.Reboot()

	// get journal lines from the current boot
	output := c.MustSSH(m, "journalctl -b 0 -o cat -u initrd-switch-root.target -u systemd-networkd.service")
	lines := strings.Split(string(output), "\n")

	// verify that the network service was started
	found := false
	for _, line := range lines {
		if line == "Started Network Service." {
			found = true
			break
		}
	}
	if !found {
		c.Fatal("couldn't find log entry for networkd startup")
	}

	// check that we exited the initramfs first
	if lines[0] != "Reached target Switch Root." {
		c.Fatal("networkd started in initramfs")
	}
}
