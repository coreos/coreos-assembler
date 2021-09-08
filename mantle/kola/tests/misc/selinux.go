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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         SelinuxEnforce,
		ClusterSize: 1,
		Name:        "coreos.selinux.enforce",
	})
	register.RegisterTest(&register.Test{
		Run:         SelinuxBoolean,
		ClusterSize: 1,
		Name:        "coreos.selinux.boolean",
	})
	register.RegisterTest(&register.Test{
		Run:         SelinuxBooleanPersist,
		ClusterSize: 1,
		Name:        "rhcos.selinux.boolean.persist",
	})
	register.RegisterTest(&register.Test{
		Run:         SelinuxManage,
		ClusterSize: 1,
		Name:        "rhcos.selinux.manage",
		Distros:     []string{"rhcos"},
	})
}

// cmdCheckOutput is used by `testSelinuxCmds()`. It contains a
// command to run, a bool indicating if output should be matched,
// and the regex used for the match
type cmdCheckOutput struct {
	cmdline     string // command to run
	checkoutput bool   // should output be checked
	match       string // regex used to match output from command
}

// seBooleanState is used by `getSelinuxBooleanState()` to store the
// original value of the boolean and the flipped value of the boolean
type seBooleanState struct {
	originalValue string // original boolean value from `getseboolean`
	newValue      string // new, opposite boolean value
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

// getSelinuxBooleanState checks the original value of a provided SELinux boolean
// and returns a seBooleanState struct with original + opposite/new value
func getSelinuxBooleanState(c cluster.TestCluster, m platform.Machine, seBool string) (seBooleanState, error) {
	boolState := seBooleanState{}

	// get original value of boolean
	reString := seBool + " --> (on|off)"
	reBool, _ := regexp.Compile(reString)
	origOut, err := c.SSH(m, "getsebool "+seBool)
	if err != nil {
		return boolState, fmt.Errorf(`Could not get SELinux boolean: %v`, err)
	}

	match := reBool.FindStringSubmatch(string(origOut))

	if match == nil {
		return boolState, fmt.Errorf(`failed to match regexp %q, from output %q`, reString, origOut)
	}

	origBool := match[1]

	if string(origBool) == "off" {
		boolState.originalValue = "off"
		boolState.newValue = "on"
	} else if string(origBool) == "on" {
		boolState.originalValue = "on"
		boolState.newValue = "off"
	} else {
		return boolState, fmt.Errorf(`failed to match boolean value; expected "on" or "off", got %q`, string(origBool))
	}

	return boolState, nil
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

	c.AssertCmdOutputMatches(m, "getenforce", regexp.MustCompile("^Enforcing$"))
}

// SelinuxBoolean checks that you can tweak a boolean in the current session
func SelinuxBoolean(c cluster.TestCluster) {
	seBoolean := "virt_use_nfs"

	m := c.Machines()[0]

	tempBoolState, err := getSelinuxBooleanState(c, m, seBoolean)
	if err != nil {
		c.Fatalf(`Failed to gather SELinux boolean state: %v`, err)
	}

	// construct a regexp that looks like ".*off" or ".*on"
	tempBoolRegexp := ".*" + tempBoolState.newValue
	cmdList := []cmdCheckOutput{
		{fmt.Sprintf("sudo setsebool %s %s", seBoolean, tempBoolState.newValue), false, ""},
		{"getsebool " + seBoolean, true, tempBoolRegexp},
	}

	testSelinuxCmds(c, m, cmdList)

	// since we didn't persist the change, should return to default value
	postOut := c.MustSSH(m, "getsebool "+seBoolean)
	postBool := strings.Split(string(postOut), " ")[2]

	// newBool[0] contains the original value of the boolean
	if postBool != tempBoolState.originalValue {
		c.Fatalf(`The SELinux boolean "%q" is incorrectly configured: wanted %q, got %q`, seBoolean, tempBoolState.originalValue, postBool)
	}
}

// SelinuxBooleanPersist checks that you can tweak a boolean and have it
// persist across reboots
func SelinuxBooleanPersist(c cluster.TestCluster) {
	seBoolean := "virt_use_nfs"

	m := c.Machines()[0]

	persistBoolState, err := getSelinuxBooleanState(c, m, seBoolean)
	if err != nil {
		c.Fatalf(`Failed to gather SELinux boolean state: %v`, err)
	}

	// construct a regexp that looks like ".*off" or ".*on"
	persistBoolRegexp := ".*" + persistBoolState.newValue
	cmdList := []cmdCheckOutput{
		{fmt.Sprintf("sudo setsebool -P %s %s", seBoolean, persistBoolState.newValue), false, ""},
		{"getsebool " + seBoolean, true, persistBoolRegexp},
	}

	testSelinuxCmds(c, m, cmdList)

	// the change should be persisted after a reboot
	postOut := c.MustSSH(m, "getsebool "+seBoolean)
	postBool := strings.Split(string(postOut), " ")[2]

	if postBool != persistBoolState.newValue {
		c.Fatalf(`The SELinux boolean "%q" is incorrectly configured: wanted %q, got %q`, seBoolean, persistBoolState.newValue, postBool)
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
	c.AssertCmdOutputMatches(m, "sudo semanage fcontext -l | grep vasd", regexp.MustCompile(".*system_u:object_r:httpd_log_t:s0"))
}
