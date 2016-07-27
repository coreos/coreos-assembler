// Copyright 2015 CoreOS, Inc.
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

package platform

import (
	"fmt"

	"github.com/aws/aws-sdk-go/service/ec2"
	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/conf"
)

type awsMachine struct {
	cluster *awsCluster
	mach    *ec2.Instance
}

func (am *awsMachine) ID() string {
	return *am.mach.InstanceId
}

func (am *awsMachine) IP() string {
	return *am.mach.PublicIpAddress
}

func (am *awsMachine) PrivateIP() string {
	return *am.mach.PrivateIpAddress
}

func (am *awsMachine) SSHClient() (*ssh.Client, error) {
	return am.cluster.SSHClient(am.IP())
}

func (am *awsMachine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return am.cluster.PasswordSSHClient(am.IP(), user, password)
}

func (am *awsMachine) SSH(cmd string) ([]byte, error) {
	return am.cluster.SSH(am, cmd)
}

func (am *awsMachine) Destroy() error {
	if err := am.cluster.api.TerminateInstance(am.ID()); err != nil {
		return err
	}

	am.cluster.DelMach(am)
	return nil
}

// AWSOptions contains AWS-specific instance options.
type AWSOptions struct {
	AMI           string
	InstanceType  string
	SecurityGroup string
	*Options
}

type awsCluster struct {
	*BaseCluster
	api  *aws.API
	conf AWSOptions
}

// NewAWSCluster creates an instance of a Cluster suitable for spawning
// instances on Amazon Web Services' Elastic Compute platform.
//
// NewAWSCluster will consume the environment variables $AWS_REGION,
// $AWS_ACCESS_KEY_ID, and $AWS_SECRET_ACCESS_KEY to determine the region to
// spawn instances in and the credentials to use to authenticate.
func NewAWSCluster(conf AWSOptions) (Cluster, error) {
	api, err := aws.New()
	if err != nil {
		return nil, err
	}

	bc, err := NewBaseCluster(conf.BaseName)
	if err != nil {
		return nil, err
	}

	ac := &awsCluster{
		BaseCluster: bc,
		api:         api,
		conf:        conf,
	}

	keys, err := ac.Keys()
	if err != nil {
		return nil, err
	}

	if err := api.AddKey(ac.name, keys[0].String()); err != nil {
		return nil, fmt.Errorf("failed to add SSH key: %v", err)
	}

	return ac, nil
}

func (ac *awsCluster) NewMachine(userdata string) (Machine, error) {
	conf, err := conf.New(userdata)
	if err != nil {
		return nil, err
	}

	keys, err := ac.Keys()
	if err != nil {
		return nil, err
	}

	conf.CopyKeys(keys)

	insts, err := ac.api.CreateInstances(ac.conf.AMI, ac.name, conf.String(), ac.conf.InstanceType, ac.conf.SecurityGroup, 1, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create EC2 instances: %v", err)
	}

	mach := &awsMachine{
		cluster: ac,
		mach:    insts[0],
	}

	if err := CheckMachine(mach); err != nil {
		return nil, fmt.Errorf("machine %q failed basic checks: %v", mach.ID(), err)
	}

	ac.AddMach(mach)

	return mach, nil
}

func (ac *awsCluster) Destroy() error {
	if err := ac.api.DeleteKey(ac.name); err != nil {
		return err
	}

	return ac.BaseCluster.Destroy()
}
