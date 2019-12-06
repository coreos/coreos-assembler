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
	register.Register(&register.Test{
		Run:         NetworkListeners,
		ClusterSize: 1,
		Name:        "fcos.network.listeners",
		Distros:     []string{"fcos"},
		// be sure to notice listeners in the docker stack
		UserData: conf.ContainerLinuxConfig(`systemd:
  units:
    - name: docker.service
      enabled: true`),
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
	// TODO: rewrite test for NetworkManager
	register.Register(&register.Test{
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

	output := c.MustSSH(m, "sudo netstat -plutn")

	processes := strings.Split(string(output), "\n")
	// verify header is as expected
	if len(processes) < 2 {
		c.Fatalf("expected at least two lines of nestat output: %q", output)
	}
	if processes[0] != "Active Internet connections (only servers)" {
		c.Fatalf("netstat output has changed format: %q", output)
	}
	if !regexp.MustCompile(`Proto\s+Recv-Q\s+Send-Q\s+Local Address\s+Foreign Address\s+State\s+PID/Program name`).MatchString(processes[1]) {
		c.Fatalf("netstat output has changed format: %q", output)
	}
	// skip header
	processes = processes[2:]

NextProcess:
	for _, line := range processes {
		parts := strings.Fields(line)
		// One gotcha: udp's 'state' field is optional, so it's possible to have 6
		// or 7 parts depending on that.
		if len(parts) != 6 && len(parts) != 7 {
			c.Fatalf("unexpected number of parts on line: %q in output %q", line, output)
		}
		proto := parts[0]
		portdata := strings.Split(parts[3], ":")
		port := portdata[len(portdata)-1]
		pidProgramParts := strings.SplitN(parts[len(parts)-1], "/", 2)
		if len(pidProgramParts) != 2 {
			c.Errorf("%v did not contain pid and program parts; full output: %q", parts[6], output)
			continue
		}
		pid, process := pidProgramParts[0], pidProgramParts[1]

		for _, expected := range expectedListeners {
			if strings.HasPrefix(proto, expected.protocol) && // allow expected tcp to match tcp6
				expected.port == port &&
				expected.process == process {
				// matches expected process
				continue NextProcess
			}
		}

		if process[0] == '(' {
			c.Logf("Ignoring %q listener process: %q (pid %s) on %q", proto, process, pid, port)
			continue
		}

		c.Logf("full netstat output: %q", output)
		return fmt.Errorf("Unexpected listener process: %q", line)
	}

	return nil
}

func NetworkListeners(c cluster.TestCluster) {
	expectedListeners := []listener{
		{"tcp", "22", "sshd"},
		{"udp", "68", "dhclient"},
		{"udp", "323", "chronyd"},
	}
	checkList := func() error {
		return checkListeners(c, expectedListeners)
	}
	if err := util.Retry(3, 5*time.Second, checkList); err != nil {
		c.Errorf(err.Error())
	}
}

// Verify that networking is not started in the initramfs on the second boot.
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
