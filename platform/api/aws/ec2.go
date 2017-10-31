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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/coreos/mantle/util"
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

// CreateInstances creates EC2 instances with a given name tag, optional ssh key name, user data. The image ID, instance type, and security group set in the API will be used. CreateInstances will block until all instances are running and have an IP address.
func (a *API) CreateInstances(name, keyname, userdata string, count uint64) ([]*ec2.Instance, error) {
	cnt := int64(count)

	var ud *string
	if len(userdata) > 0 {
		tud := base64.StdEncoding.EncodeToString([]byte(userdata))
		ud = &tud
	}

	err := a.ensureInstanceProfile(a.opts.IAMInstanceProfile)
	if err != nil {
		return nil, fmt.Errorf("error verifying IAM instance profile: %v", err)
	}

	sgId, err := a.getSecurityGroupID(a.opts.SecurityGroup)
	if err != nil {
		return nil, fmt.Errorf("error resolving security group: %v", err)
	}
	key := &keyname
	if keyname == "" {
		key = nil
	}
	inst := ec2.RunInstancesInput{
		ImageId:          &a.opts.AMI,
		MinCount:         &cnt,
		MaxCount:         &cnt,
		KeyName:          key,
		InstanceType:     &a.opts.InstanceType,
		SecurityGroupIds: []*string{&sgId},
		UserData:         ud,
		IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
			Name: &a.opts.IAMInstanceProfile,
		},
		TagSpecifications: []*ec2.TagSpecification{
			&ec2.TagSpecification{
				ResourceType: aws.String(ec2.ResourceTypeInstance),
				Tags: []*ec2.Tag{
					&ec2.Tag{
						Key:   aws.String("Name"),
						Value: aws.String(name),
					},
					&ec2.Tag{
						Key:   aws.String("CreatedBy"),
						Value: aws.String("mantle"),
					},
				},
			},
		},
	}

	reservations, err := a.ec2.RunInstances(&inst)
	if err != nil {
		return nil, fmt.Errorf("error running instances: %v", err)
	}

	ids := make([]string, len(reservations.Instances))
	for i, inst := range reservations.Instances {
		ids[i] = *inst.InstanceId
	}

	// loop until all machines are online
	var insts []*ec2.Instance

	// 5 minutes is a pretty reasonable timeframe for AWS instances to work.
	timeout := 5 * time.Minute
	// don't make api calls too quickly, or we will hit the rate limit
	delay := 10 * time.Second
	err = util.WaitUntilReady(timeout, delay, func() (bool, error) {
		desc, err := a.ec2.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice(ids),
		})
		if err != nil {
			return false, err
		}
		insts = desc.Reservations[0].Instances

		for _, i := range insts {
			if *i.State.Name != ec2.InstanceStateNameRunning || i.PublicIpAddress == nil {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		a.TerminateInstances(ids)
		return nil, fmt.Errorf("waiting for instances to run: %v", err)
	}

	return insts, nil
}

// TerminateInstances schedules EC2 instances to be terminated.
func (a *API) TerminateInstances(ids []string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice(ids),
	}

	if _, err := a.ec2.TerminateInstances(input); err != nil {
		return err
	}

	return nil
}

func (a *API) CreateTags(resources []string, tags map[string]string) error {
	tagObjs := make([]*ec2.Tag, 0, len(tags))
	for key, value := range tags {
		tagObjs = append(tagObjs, &ec2.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}
	_, err := a.ec2.CreateTags(&ec2.CreateTagsInput{
		Resources: aws.StringSlice(resources),
		Tags:      tagObjs,
	})
	if err != nil {
		return fmt.Errorf("error creating tags: %v", err)
	}
	return err
}

func (a *API) GetConsoleOutput(instanceID string, wait bool) (string, error) {
	var output string

	err := util.Retry(60, 5*time.Second, func() error {
		res, err := a.ec2.GetConsoleOutput(&ec2.GetConsoleOutputInput{
			InstanceId: aws.String(instanceID),
		})
		if err != nil {
			return fmt.Errorf("couldn't get console output of %v: %v", instanceID, err)
		}

		if res.Output == nil {
			if wait {
				plog.Debugf("waiting for console for %v", instanceID)
				return fmt.Errorf("timed out waiting for console output of %v", instanceID)
			} else {
				return nil
			}
		}

		decoded, err := base64.StdEncoding.DecodeString(*res.Output)
		if err != nil {
			return fmt.Errorf("couldn't decode console output of %v: %v", instanceID, err)
		}

		output = string(decoded)
		return nil
	})

	return output, err
}

// getSecurityGroupID gets a security group matching the given name.
// If the security group does not exist, it's created.
func (a *API) getSecurityGroupID(name string) (string, error) {
	sgIds, err := a.ec2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupNames: []*string{&name},
	})
	if isSecurityGroupNotExist(err) {
		return a.createSecurityGroup(name)
	}

	if err != nil {
		return "", fmt.Errorf("unable to get security group named %v: %v", name, err)
	}
	if len(sgIds.SecurityGroups) == 0 {
		return "", fmt.Errorf("zero security groups matched name %v", name)
	}
	return *sgIds.SecurityGroups[0].GroupId, nil
}

// createSecurityGroup creates a security group with tcp/22 access allowed from the
// internet.
func (a *API) createSecurityGroup(name string) (string, error) {
	sg, err := a.ec2.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(name),
		Description: aws.String("mantle security group for testing"),
	})
	if err != nil {
		return "", err
	}
	plog.Debugf("created security group %v", *sg.GroupId)

	allowedIngresses := []ec2.AuthorizeSecurityGroupIngressInput{
		{
			// SSH access from the public internet
			GroupId: sg.GroupId,
			IpPermissions: []*ec2.IpPermission{
				{
					IpProtocol: aws.String("tcp"),
					IpRanges: []*ec2.IpRange{
						{
							CidrIp: aws.String("0.0.0.0/0"),
						},
					},
					FromPort: aws.Int64(22),
					ToPort:   aws.Int64(22),
				},
			},
		},
		{
			// Access from all things in this vpc with the same SG (e.g. other
			// machines in our kola cluster)
			GroupId:                 sg.GroupId,
			SourceSecurityGroupName: aws.String(name),
		},
	}

	for _, input := range allowedIngresses {
		_, err := a.ec2.AuthorizeSecurityGroupIngress(&input)

		if err != nil {
			// We created the SG but can't add all the needed rules, let's try to
			// bail gracefully
			_, delErr := a.ec2.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			})
			if delErr != nil {
				return "", fmt.Errorf("created sg %v (%v) but couldn't authorize it. Manual deletion may be required: %v", *sg.GroupId, name, err)
			}
			return "", fmt.Errorf("created sg %v (%v), but couldn't authorize it and thus deleted it: %v", *sg.GroupId, name, err)
		}
	}
	return *sg.GroupId, err
}

func isSecurityGroupNotExist(err error) bool {
	if err == nil {
		return false
	}
	if awsErr, ok := (err).(awserr.Error); ok {
		if awsErr.Code() == "InvalidGroup.NotFound" {
			return true
		}
	}
	return false
}
