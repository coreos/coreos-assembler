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

func (am *machine) Reboot() error {
	return platform.RebootMachine(am, am.journal)
}

func (am *machine) Destroy() {
	origConsole, err := am.cluster.api.GetConsoleOutput(am.ID(), false)
	if err != nil {
		plog.Warningf("Error retrieving console log for %v: %v", am.ID(), err)
	}

	if err := am.cluster.api.TerminateInstances([]string{am.ID()}); err != nil {
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
	// am.cluster.api.GetConsoleOutput(am.ID(), true) will loop until
	// the returned console output is non-empty. However, if the
	// instance has e.g. been running for several minutes, the returned
	// output will be non-empty but won't necessarily include the most
	// recent log messages.  So we loop until the post-termination logs
	// are different from the pre-termination logs.
	err := util.Retry(60, 5*time.Second, func() error {
		var err error
		am.console, err = am.cluster.api.GetConsoleOutput(am.ID(), false)
		if err != nil {
			return err
		}

		if am.console == origConsole {
			plog.Debugf("waiting for console for %v", am.ID())
			return fmt.Errorf("timed out waiting for console output of %v", am.ID())
		}

		return nil
	})
	if err != nil {
		return err
	}

	path := filepath.Join(am.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	f.WriteString(am.console)

	return nil
}
