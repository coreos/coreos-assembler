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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/coreos/mantle/platform"
)

type Options struct {
	*platform.Options
	AMI           string
	InstanceType  string
	SecurityGroup string
}

type API struct {
	session *session.Session
	ec2     *ec2.EC2
	opts    *Options
}

// New creates a new AWS API wrapper. It uses credentials from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables.
func New(opts *Options) (*API, error) {
	creds := credentials.NewEnvCredentials()
	if _, err := creds.Get(); err != nil {
		return nil, fmt.Errorf("no AWS credentials provided: %v", err)
	}

	cfg := aws.NewConfig().WithCredentials(creds)

	sess := session.New(cfg)

	api := &API{
		session: sess,
		ec2:     ec2.New(sess),
		opts:    opts,
	}

	return api, nil
}
