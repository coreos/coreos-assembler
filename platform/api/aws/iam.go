// Copyright 2017 CoreOS, Inc.
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
	"github.com/aws/aws-sdk-go/service/iam"
)

const (
	ec2AssumeRolePolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "",
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}`
)

// ensureInstanceProfile checks that the specified instance profile exists,
// and creates it and its backing role if not. The role will have no access
// policy.
func (a *API) ensureInstanceProfile(name string) error {
	_, err := a.iam.GetInstanceProfile(&iam.GetInstanceProfileInput{
		InstanceProfileName: &name,
	})
	if err == nil {
		return nil
	}
	if awserr, ok := err.(awserr.Error); !ok || awserr.Code() != "NoSuchEntity" {
		return fmt.Errorf("getting instance profile %q: %v", name, err)
	}

	_, err = a.iam.CreateRole(&iam.CreateRoleInput{
		RoleName:                 &name,
		Description:              aws.String("mantle role for testing"),
		AssumeRolePolicyDocument: aws.String(ec2AssumeRolePolicy),
	})
	if err != nil {
		return fmt.Errorf("creating role %q: %v", name, err)
	}

	_, err = a.iam.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
		InstanceProfileName: &name,
	})
	if err != nil {
		return fmt.Errorf("creating instance profile %q: %v", name, err)
	}

	_, err = a.iam.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: &name,
		RoleName:            &name,
	})
	if err != nil {
		return fmt.Errorf("adding role %q to instance profile %q: %v", name, name, err)
	}

	return nil
}
