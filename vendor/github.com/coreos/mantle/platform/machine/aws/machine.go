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
	"github.com/aws/aws-sdk-go/service/ec2"
	"golang.org/x/crypto/ssh"
)

type machine struct {
	cluster *cluster
	mach    *ec2.Instance
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

func (am *machine) SSHClient() (*ssh.Client, error) {
	return am.cluster.SSHClient(am.IP())
}

func (am *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return am.cluster.PasswordSSHClient(am.IP(), user, password)
}

func (am *machine) SSH(cmd string) ([]byte, error) {
	return am.cluster.SSH(am, cmd)
}

func (am *machine) Destroy() error {
	if err := am.cluster.api.TerminateInstance(am.ID()); err != nil {
		return err
	}

	am.cluster.DelMach(am)
	return nil
}
