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
	"regexp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         SelinuxEnforce,
		ClusterSize: 1,
		Name:        "coreos.selinux.enforce",
		Distros:     []string{"cl", "rhcos", "fcos"},
	})
	register.Register(&register.Test{
		Run:         SelinuxBoolean,
		ClusterSize: 1,
		Name:        "coreos.selinux.boolean",
		Distros:     []string{"cl", "rhcos", "fcos"},
	})
	register.Register(&register.Test{
		Run:         SelinuxBooleanPersist,
		ClusterSize: 1,
		Name:        "rhcos.selinux.boolean.persist",
		Distros:     []string{"rhcos", "fcos"},
	})
	register.Register(&register.Test{
		Run:         SelinuxManage,
		ClusterSize: 1,
		Name:        "rhcos.selinux.manage",
		Distros:     []string{"rhcos", "fcos"},
	})
}

type cmdCheckOutput struct {
	cmdline     string // command to run
	checkoutput bool   // should output be checked
	match       string // regex used to match output from command
}

// testSelinuxCmds will run a list of commands, optionally check their output, and
// ultimately reboot the host
func testSelinuxCmds(c cluster.TestCluster, m platform.Machine, cmds []cmdCheckOutput) {
	for _, cmd := range cmds {
		output := c.MustSSH(m, cmd.cmdline)

		if cmd.checkoutput {
			match := regexp.MustCompile(cmd.match).MatchString(string(output))
			if !match {
				c.Fatalf("command %q has unexpected output: tried to match regexp %q, output was %q", cmd.cmdline, cmd.match, string(output))
			}
		}
	}

	err := m.Reboot()
	if err != nil {
		c.Fatalf("failed to reboot machine: %v", err)
	}
}

// SelinuxEnforce checks that some basic things work after `setenforce 1`
func SelinuxEnforce(c cluster.TestCluster) {
	cmdList := []cmdCheckOutput{
		{"getenforce", true, "Enforcing"},
		{"sudo setenforce 0", false, ""},
		{"getenforce", true, "Permissive"},
		{"sudo setenforce 1", false, ""},
		{"getenforce", true, "Enforcing"},
		{"systemctl --no-pager is-active system.slice", true, "active"},
		{"sudo cp /etc/selinux/config{,.old}", false, ""},
		{"sudo sed -i 's/SELINUX=permissive/SELINUX=enforcing/' /etc/selinux/config", false, ""},
	}

	m := c.Machines()[0]

	testSelinuxCmds(c, m, cmdList)

	output := c.MustSSH(m, "getenforce")

	if string(output) != "Enforcing" {
		c.Fatalf(`command "getenforce" has unexpected output: want %q, got %q`, "Enforcing", string(output))
	}
}

// SelinuxBoolean checks that you can tweak a boolean in the current session
func SelinuxBoolean(c cluster.TestCluster) {
	cmdList := []cmdCheckOutput{
		{"getsebool virt_use_nfs", true, ".*off"},
		{"sudo setsebool virt_use_nfs 1", false, ""},
		{"getsebool virt_use_nfs", true, ".*on"},
	}

	m := c.Machines()[0]

	testSelinuxCmds(c, m, cmdList)

	// since we didn't persist the change, should return to default value
	output := c.MustSSH(m, "getsebool virt_use_nfs")

	if string(output) != "virt_use_nfs --> off" {
		c.Fatalf(`The SELinux boolean "virt_use_nfs" is incorrectly configured: want %q, got %q`, "virt_use_nfs --> off", string(output))
	}
}

// SelinuxBooleanPersist checks that you can tweak a boolean and have it
// persist across reboots
func SelinuxBooleanPersist(c cluster.TestCluster) {
	cmdList := []cmdCheckOutput{
		{"getsebool virt_use_nfs", true, ".*off"},
		{"sudo setsebool -P virt_use_nfs 1", false, ""},
		{"getsebool virt_use_nfs", true, ".*on"},
	}

	m := c.Machines()[0]

	testSelinuxCmds(c, m, cmdList)

	// the change should be persisted after a reboot
	output := c.MustSSH(m, "getsebool virt_use_nfs")

	if string(output) != "virt_use_nfs --> on" {
		c.Fatalf(`The SELinux boolean "virt_use_nfs" is incorrectly configured: want %q, got %q`, "virt_use_nfs --> on", string(output))
	}
}

// SelinuxManage checks that you can modify an SELinux file context and
// have it persist across reboots
func SelinuxManage(c cluster.TestCluster) {
	cmdList := []cmdCheckOutput{
		{"sudo semanage fcontext -l | grep vasd", true, ".*system_u:object_r:var_auth_t:s0"},
		{"sudo semanage fcontext -m -t httpd_log_t \"/var/opt/quest/vas/vasd(/.*)?\"", false, ""},
		{"sudo semanage fcontext -l | grep vasd", true, ".*system_u:object_r:httpd_log_t:s0"},
	}

	m := c.Machines()[0]

	testSelinuxCmds(c, m, cmdList)

	// the change should be persisted after a reboot
	output := c.MustSSH(m, "sudo semanage fcontext -l | grep vasd")

	s := ".*system_u:object_r:httpd_log_t:s0"
	match := regexp.MustCompile(s).MatchString(string(output))
	if !match {
		c.Fatalf(`The SELinux file context "/var/opt/quest/vas/vasd(/.*)?" is incorrectly configured.  Tried to match regexp %q, output was %q`, s, string(output))
	}
}
