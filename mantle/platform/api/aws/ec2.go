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
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/coreos/mantle/util"
)

type RegionKind int

const (
	RegionEnabled RegionKind = iota
	RegionDisabled
	RegionAny
)

// ListRegions lists the enabled regions in the AWS partition specified
// implicitly by the CredentialsFile, Profile, and Region options.
func (a *API) ListRegions(kind RegionKind) ([]string, error) {
	input := ec2.DescribeRegionsInput{}
	switch kind {
	case RegionDisabled:
		input.AllRegions = aws.Bool(true)
		input.Filters = []*ec2.Filter{
			{
				Name:   aws.String("opt-in-status"),
				Values: []*string{aws.String("not-opted-in")},
			},
		}
	case RegionAny:
		input.AllRegions = aws.Bool(true)
	}
	output, err := a.ec2.DescribeRegions(&input)
	if err != nil {
		return nil, fmt.Errorf("describing regions: %v", err)
	}
	ret := make([]string, 0, len(output.Regions))
	for _, region := range output.Regions {
		ret = append(ret, *region.RegionName)
	}
	sort.Strings(ret)
	return ret, nil
}

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
func (a *API) CreateInstances(name, keyname, userdata string, count uint64, minDiskSize int64, useInstanceProfile bool) ([]*ec2.Instance, error) {
	cnt := int64(count)

	var ud *string
	if len(userdata) > 0 {
		tud := base64.StdEncoding.EncodeToString([]byte(userdata))
		ud = &tud
	}

	if useInstanceProfile {
		err := a.ensureInstanceProfile(a.opts.IAMInstanceProfile)
		if err != nil {
			return nil, fmt.Errorf("error verifying IAM instance profile: %v", err)
		}
	}

	sgId, err := a.getSecurityGroupID(a.opts.SecurityGroup)
	if err != nil {
		return nil, fmt.Errorf("error resolving security group: %v", err)
	}

	vpcId, err := a.getVPCID(sgId)
	if err != nil {
		return nil, fmt.Errorf("error resolving vpc: %v", err)
	}

	zones, err := a.GetZonesForInstanceType(a.opts.InstanceType)
	if err != nil {
		// Find all available zones that offer the given instance type
		return nil, fmt.Errorf("error finding zones for instance type %v", a.opts.InstanceType)
	}

	var reservations *ec2.Reservation

	// Iterate over other possible zones if capacity for an instance
	// type is exhausted
	for zoneKey, zone := range zones {
		subnetId, err := a.getSubnetID(vpcId, zone)
		if err != nil {
			return nil, fmt.Errorf("error resolving subnet: %v", err)
		}

		key := &keyname
		if keyname == "" {
			key = nil
		}

		var rootBlockDev []*ec2.BlockDeviceMapping
		if minDiskSize > 0 {
			rootBlockDev = append(rootBlockDev, &ec2.BlockDeviceMapping{
				DeviceName: aws.String("/dev/xvda"),
				Ebs: &ec2.EbsBlockDevice{
					VolumeSize: &minDiskSize,
				},
			})
		}
		inst := ec2.RunInstancesInput{
			ImageId:             &a.opts.AMI,
			MinCount:            &cnt,
			MaxCount:            &cnt,
			KeyName:             key,
			InstanceType:        &a.opts.InstanceType,
			SecurityGroupIds:    []*string{&sgId},
			SubnetId:            &subnetId,
			UserData:            ud,
			BlockDeviceMappings: rootBlockDev,
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: aws.String(ec2.ResourceTypeInstance),
					Tags: []*ec2.Tag{
						{
							Key:   aws.String("Name"),
							Value: aws.String(name),
						},
						{
							Key:   aws.String("CreatedBy"),
							Value: aws.String("mantle"),
						},
					},
				},
			},
		}
		if useInstanceProfile {
			inst.IamInstanceProfile = &ec2.IamInstanceProfileSpecification{
				Name: &a.opts.IAMInstanceProfile,
			}
		}

		err = util.RetryConditional(5, 5*time.Second, func(err error) bool {
			// due to AWS' eventual consistency despite ensuring that the IAM Instance
			// Profile has been created it may not be available to ec2 yet.
			if awsErr, ok := err.(awserr.Error); ok && (awsErr.Code() == "InvalidParameterValue" && strings.Contains(awsErr.Message(), "iamInstanceProfile.name")) {
				return true
			}
			return false
		}, func() error {
			var ierr error
			reservations, ierr = a.ec2.RunInstances(&inst)
			return ierr
		})
		if err == nil {
			// Successfully started our instance in the requested zone. Break out of the loop
			break
		}
		if err != nil {
			// Handle InsufficientInstanceCapacity error specifically
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "InsufficientInstanceCapacity" {
				// If we iterate over all possible zones and none of them have sufficient instance(s)
				// available we will return the InsufficientInstanceCapacity error
				if zoneKey == len(zones)-1 {
					return nil, fmt.Errorf("all available zones tried: %v", err)
				}
				plog.Warningf("Insufficient instances available in zone %v. Trying the next zone\n", zone)
				continue
			}
			return nil, fmt.Errorf("error running instances: %v", err)
		}
	}

	ids := make([]string, len(reservations.Instances))
	for i, inst := range reservations.Instances {
		ids[i] = *inst.InstanceId
	}

	// loop until all machines are online
	var insts []*ec2.Instance

	// 10 minutes is a pretty reasonable timeframe for AWS instances to work.
	timeout := 10 * time.Minute
	// don't make api calls too quickly, or we will hit the rate limit
	delay := 10 * time.Second
	err = util.WaitUntilReady(timeout, delay, func() (bool, error) {
		desc, err := a.ec2.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice(ids),
		})
		if err != nil {
			// Keep retrying if the InstanceID disappears momentarily
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "InvalidInstanceID.NotFound" {
				plog.Debugf("instance ID not found, retrying: %v", err)
				return false, nil
			}
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
		if errTerminate := a.TerminateInstances(ids); errTerminate != nil {
			return nil, fmt.Errorf("terminating instances failed: %v after instances failed to run: %v", errTerminate, err)
		}
		return nil, fmt.Errorf("waiting for instances to run: %v", err)
	}

	return insts, nil
}

// gcEC2 will terminate ec2 instances older than gracePeriod.
// It will only operate on ec2 instances tagged with 'mantle' to avoid stomping
// on other resources in the account.
func (a *API) gcEC2(gracePeriod time.Duration) error {
	durationAgo := time.Now().Add(-1 * gracePeriod)

	instances, err := a.ec2.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:CreatedBy"),
				Values: aws.StringSlice([]string{"mantle"}),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("error describing instances: %v", err)
	}

	toTerminate := []string{}

	for _, reservation := range instances.Reservations {
		for _, instance := range reservation.Instances {
			if instance.LaunchTime.After(durationAgo) {
				plog.Debugf("ec2: skipping instance %s due to being too new", *instance.InstanceId)
				// Skip, still too new
				continue
			}

			if instance.State != nil {
				switch *instance.State.Name {
				case ec2.InstanceStateNamePending, ec2.InstanceStateNameRunning, ec2.InstanceStateNameStopped:
					toTerminate = append(toTerminate, *instance.InstanceId)
				case ec2.InstanceStateNameTerminated, ec2.InstanceStateNameShuttingDown:
				default:
					plog.Infof("ec2: skipping instance in state %s", *instance.State.Name)
				}
			} else {
				plog.Warningf("ec2 instance had no state: %s", *instance.InstanceId)
			}
		}
	}

	return a.TerminateInstances(toTerminate)
}

// TerminateInstances schedules EC2 instances to be terminated.
func (a *API) TerminateInstances(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	input := &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice(ids),
	}

	if _, err := a.ec2.TerminateInstances(input); err != nil {
		return err
	}

	return nil
}

func (a *API) CreateTags(resources []string, tags map[string]string) error {
	if len(tags) == 0 {
		return nil
	}

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

// GetConsoleOutput returns the console output. Returns "", nil if no logs
// are available.
func (a *API) GetConsoleOutput(instanceID string) (string, error) {
	res, err := a.ec2.GetConsoleOutput(&ec2.GetConsoleOutputInput{
		InstanceId: aws.String(instanceID),
	})
	if err != nil {
		return "", fmt.Errorf("couldn't get console output of %v: %v", instanceID, err)
	}

	if res.Output == nil {
		return "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(*res.Output)
	if err != nil {
		return "", fmt.Errorf("couldn't decode console output of %v: %v", instanceID, err)
	}

	return string(decoded), nil
}

// GetZonesForInstanceType returns all available zones that offer the
// given instance type. This is useful because not all availability zones
// offer all instances types.
func (a *API) GetZonesForInstanceType(instanceType string) ([]string, error) {

	input := ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: aws.String(ec2.LocationTypeAvailabilityZone),
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance-type"),
				Values: []*string{aws.String(instanceType)},
			},
		},
	}
	output, err := a.ec2.DescribeInstanceTypeOfferings(&input)
	if err != nil {
		return nil, fmt.Errorf("error describing instance offerings: %v", err)
	}
	if len(output.InstanceTypeOfferings) == 0 {
		return nil, fmt.Errorf("no availability zones found for this instance type %v:", instanceType)
	}

	var zones []string
	for _, v := range output.InstanceTypeOfferings {
		zones = append(zones, *v.Location)
	}
	return zones, nil
}
