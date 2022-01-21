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

package aws

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

type machine struct {
	cluster *cluster
	mach    *ec2.Instance
	dir     string
	journal *platform.Journal
	console string
}

func (am *machine) ID() string {
	return *am.mach.InstanceId
}

func (am *machine) IP() string {
	return *am.mach.PublicIpAddress
}

func (am *machine) PrivateIP() string {
	return *am.mach.PrivateIpAddress
}

func (am *machine) RuntimeConf() platform.RuntimeConfig {
	return am.cluster.RuntimeConf()
}

func (am *machine) SSHClient() (*ssh.Client, error) {
	return am.cluster.SSHClient(am.IP())
}

func (am *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return am.cluster.PasswordSSHClient(am.IP(), user, password)
}

func (am *machine) SSH(cmd string) ([]byte, []byte, error) {
	return am.cluster.SSH(am, cmd)
}

func (am *machine) IgnitionError() error {
	return nil
}

func (am *machine) Start() error {
	return platform.StartMachine(am, am.journal)
}

func (am *machine) Reboot() error {
	return platform.RebootMachine(am, am.journal)
}

func (am *machine) WaitForReboot(timeout time.Duration, oldBootId string) error {
	return platform.WaitForMachineReboot(am, am.journal, timeout, oldBootId)
}

func (am *machine) Destroy() {
	origConsole, err := am.cluster.flight.api.GetConsoleOutput(am.ID())
	if err != nil {
		plog.Warningf("Error retrieving console log for %v: %v", am.ID(), err)
	}

	if err := am.cluster.flight.api.TerminateInstances([]string{am.ID()}); err != nil {
		plog.Errorf("Error terminating instance %v: %v", am.ID(), err)
	}

	if am.journal != nil {
		am.journal.Destroy()
	}

	if err := am.saveConsole(origConsole); err != nil {
		plog.Errorf("Error saving console for instance %v: %v", am.ID(), err)
	}

	am.cluster.DelMach(am)
}

func (am *machine) ConsoleOutput() string {
	return am.console
}

func (am *machine) saveConsole(origConsole string) error {
	// If the instance has e.g. been running for several minutes, the
	// returned output will be non-empty but won't necessarily include
	// the most recent log messages. So we loop until the post-termination
	// logs are different from the pre-termination logs.
	err := util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		am.console, err = am.cluster.flight.api.GetConsoleOutput(am.ID())
		if err != nil {
			return false, err
		}

		if am.console == origConsole {
			plog.Debugf("waiting for post-terminate console for %v", am.ID())
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		err = fmt.Errorf("retrieving post-terminate console output of %v: %v", am.ID(), err)
		if origConsole != "" {
			plog.Warning(err)
		} else {
			return err
		}
	}

	// merge the two logs
	overlapLen := 100
	if len(am.console) < overlapLen {
		overlapLen = len(am.console)
	}
	origIdx := strings.LastIndex(origConsole, am.console[0:overlapLen])
	if origIdx != -1 {
		// overlap
		am.console = origConsole[0:origIdx] + am.console
	} else if origConsole != "" {
		// two logs with no overlap; add scissors
		am.console = origConsole + "\n\n8<------------------------\n\n" + am.console
	}

	path := filepath.Join(am.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(am.console); err != nil {
		return err
	}

	return nil
}

func (am *machine) JournalOutput() string {
	if am.journal == nil {
		return ""
	}

	data, err := am.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for instance %v: %v", am.ID(), err)
	}
	return string(data)
}
