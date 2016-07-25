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
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"golang.org/x/crypto/ssh"

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
	id := am.ID()

	input := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{&id},
	}

	if _, err := am.cluster.api.TerminateInstances(input); err != nil {
		return err
	}

	am.cluster.delMach(am)
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
	*baseCluster
	api  *ec2.EC2
	conf AWSOptions
}

// NewAWSCluster creates an instance of a Cluster suitable for spawning
// instances on Amazon Web Services' Elastic Compute platform.
//
// NewAWSCluster will consume the environment variables $AWS_REGION,
// $AWS_ACCESS_KEY_ID, and $AWS_SECRET_ACCESS_KEY to determine the region to
// spawn instances in and the credentials to use to authenticate.
func NewAWSCluster(conf AWSOptions) (Cluster, error) {
	cfg := aws.NewConfig().WithCredentials(credentials.NewEnvCredentials())
	api := ec2.New(session.New(cfg))

	bc, err := newBaseCluster(conf.BaseName)
	if err != nil {
		return nil, err
	}

	keys, err := bc.agent.List()
	if err != nil {
		return nil, err
	}

	_, err = api.ImportKeyPair(&ec2.ImportKeyPairInput{
		KeyName:           &bc.name,
		PublicKeyMaterial: []byte(keys[0].String()),
	})
	if err != nil {
		return nil, err
	}

	ac := &awsCluster{
		baseCluster: bc,
		api:         api,
		conf:        conf,
	}

	return ac, nil
}

func (ac *awsCluster) NewMachine(userdata string) (Machine, error) {
	conf, err := conf.New(userdata)
	if err != nil {
		return nil, err
	}

	keys, err := ac.agent.List()
	if err != nil {
		return nil, err
	}

	conf.CopyKeys(keys)

	var ud *string
	if cfStr := conf.String(); len(cfStr) > 0 {
		tud := base64.StdEncoding.EncodeToString([]byte(cfStr))
		ud = &tud
	}
	cnt := int64(1)

	inst := ec2.RunInstancesInput{
		ImageId:        &ac.conf.AMI,
		MinCount:       &cnt,
		MaxCount:       &cnt,
		KeyName:        &ac.name,
		InstanceType:   &ac.conf.InstanceType,
		SecurityGroups: []*string{&ac.conf.SecurityGroup},
		UserData:       ud,
	}

	resp, err := ac.api.RunInstances(&inst)
	if err != nil {
		return nil, err
	}

	ids := []*string{resp.Instances[0].InstanceId}

	if err := waitForAWSInstances(ac.api, ids, 5*time.Minute); err != nil {
		return nil, err
	}

	getinst := &ec2.DescribeInstancesInput{
		InstanceIds: ids,
	}

	insts, err := ac.api.DescribeInstances(getinst)
	if err != nil {
		return nil, err
	}

	mach := &awsMachine{
		cluster: ac,
		mach:    insts.Reservations[0].Instances[0],
	}

	if err := commonMachineChecks(mach); err != nil {
		return nil, fmt.Errorf("machine %q failed basic checks: %v", mach.ID(), err)
	}

	ac.addMach(mach)

	return mach, nil
}

func (ac *awsCluster) Destroy() error {
	_, err := ac.api.DeleteKeyPair(&ec2.DeleteKeyPairInput{
		KeyName: &ac.name,
	})
	if err != nil {
		return err
	}

	machs := ac.Machines()
	for _, am := range machs {
		am.Destroy()
	}
	ac.agent.Close()

	return nil
}

// waitForAWSInstance waits until a set of aws ec2 instance is accessible by ssh.
func waitForAWSInstances(api *ec2.EC2, ids []*string, d time.Duration) error {
	after := time.After(d)

	online := make(map[string]bool)

	for len(ids) != len(online) {
		select {
		case <-after:
			return fmt.Errorf("timed out waiting for instances to run")
		default:
		}

		// don't make api calls too quickly, or we will hit the rate limit

		time.Sleep(10 * time.Second)

		getinst := &ec2.DescribeInstancesInput{
			InstanceIds: ids,
		}

		insts, err := api.DescribeInstances(getinst)
		if err != nil {
			return err
		}

		for _, r := range insts.Reservations {
			for _, i := range r.Instances {
				// skip instances known to be up
				if online[*i.InstanceId] {
					continue
				}

				// "running"
				if *i.State.Code == int64(16) {
					// XXX: ssh is a terrible way to check this, but it is all we have.
					c, err := net.DialTimeout("tcp", *i.PublicIpAddress+":22", 10*time.Second)
					if err != nil {
						continue
					}
					c.Close()

					online[*i.InstanceId] = true
				}
			}
		}
	}

	return nil
}
