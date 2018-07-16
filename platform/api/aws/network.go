// Copyright 2018 CoreOS, Inc.
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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

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
