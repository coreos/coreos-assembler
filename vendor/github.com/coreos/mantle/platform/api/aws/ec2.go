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
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
)

func (a *API) AddKey(name, key string) error {
	_, err := a.ec2.ImportKeyPair(&ec2.ImportKeyPairInput{
		KeyName:           &name,
		PublicKeyMaterial: []byte(key),
	})

	return err
}

func (a *API) DeleteKey(name string) error {
	_, err := a.ec2.DeleteKeyPair(&ec2.DeleteKeyPairInput{
		KeyName: &name,
	})

	return err
}

// CheckInstances waits until a set of EC2 instances are accessible by SSH, waiting a maximum of 'd' time.
func (a *API) CheckInstances(ids []*string, d time.Duration) error {
	after := time.After(d)
	online := make(map[string]bool)

	// loop until all machines are online
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

		insts, err := a.ec2.DescribeInstances(getinst)
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

// CreateInstances creates EC2 instances with a given ssh key name, user data. The image ID, instance type, and security group set in the API will be used. If wait is true, CreateInstances will block until all instances are reachable by SSH.
func (a *API) CreateInstances(keyname, userdata string, count uint64, wait bool) ([]*ec2.Instance, error) {
	cnt := int64(count)

	var ud *string
	if len(userdata) > 0 {
		tud := base64.StdEncoding.EncodeToString([]byte(userdata))
		ud = &tud
	}

	inst := ec2.RunInstancesInput{
		ImageId:        &a.opts.AMI,
		MinCount:       &cnt,
		MaxCount:       &cnt,
		KeyName:        &keyname,
		InstanceType:   &a.opts.InstanceType,
		SecurityGroups: []*string{&a.opts.SecurityGroup},
		UserData:       ud,
	}

	reservations, err := a.ec2.RunInstances(&inst)
	if err != nil {
		return nil, err
	}

	if !wait {
		return reservations.Instances, nil
	}

	ids := make([]*string, len(reservations.Instances))
	for i, inst := range reservations.Instances {
		ids[i] = inst.InstanceId
	}

	// 5 minutes is a pretty reasonable timeframe for AWS instances to work.
	if err := a.CheckInstances(ids, 5*time.Minute); err != nil {
		return nil, err
	}

	// call DescribeInstances to get machine IP
	getinst := &ec2.DescribeInstancesInput{
		InstanceIds: ids,
	}

	insts, err := a.ec2.DescribeInstances(getinst)
	if err != nil {
		return nil, err
	}

	return insts.Reservations[0].Instances, err
}

// TerminateInstance schedules an EC2 instance to be terminated.
func (a *API) TerminateInstance(id string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{&id},
	}

	if _, err := a.ec2.TerminateInstances(input); err != nil {
		return err
	}

	return nil
}
