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
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go"

	"github.com/coreos/coreos-assembler/mantle/util"
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
	s3ReadOnlyAccess = `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:Get*",
                "s3:List*"
            ],
            "Resource": "*"
        }
    ]
}`
)

// ensureInstanceProfile checks that the specified instance profile exists,
// and creates it and its backing role if not. The role will have the
// AmazonS3RReadOnlyAccess permissions policy applied to allow fetches
// of S3 objects that are not owned by the root account.
func (a *API) ensureInstanceProfile(name string) error {
	_, err := a.iam.GetInstanceProfile(context.Background(), &iam.GetInstanceProfileInput{
		InstanceProfileName: &name,
	})
	if err == nil {
		return nil
	}
	var ae smithy.APIError
	if !errors.As(err, &ae) || ae.ErrorCode() != "NoSuchEntity" {
		return fmt.Errorf("getting instance profile %q: %v", name, err)
	}

	_, err = a.iam.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 &name,
		Description:              aws.String("mantle role for testing"),
		AssumeRolePolicyDocument: aws.String(ec2AssumeRolePolicy),
	})
	if err != nil {
		return fmt.Errorf("creating role %q: %v", name, err)
	}
	policy := "AmazonS3ReadOnlyAccess"
	_, err = a.iam.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
		PolicyName:     &policy,
		PolicyDocument: aws.String(s3ReadOnlyAccess),
		RoleName:       &name,
	})
	if err != nil {
		return fmt.Errorf("adding %q policy to role %q: %v", policy, name, err)
	}

	_, err = a.iam.CreateInstanceProfile(context.Background(), &iam.CreateInstanceProfileInput{
		InstanceProfileName: &name,
	})
	if err != nil {
		return fmt.Errorf("creating instance profile %q: %v", name, err)
	}

	_, err = a.iam.AddRoleToInstanceProfile(context.Background(), &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: &name,
		RoleName:            &name,
	})
	if err != nil {
		return fmt.Errorf("adding role %q to instance profile %q: %v", name, name, err)
	}

	// wait for instance profile to fully exist in IAM before returning.
	// note that this does not guarantee that it will exist within ec2.
	err = util.WaitUntilReady(30*time.Second, 5*time.Second, func() (bool, error) {
		_, err = a.iam.GetInstanceProfile(context.Background(), &iam.GetInstanceProfileInput{
			InstanceProfileName: &name,
		})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for instance profile to become ready: %v", err)
	}

	return nil
}
