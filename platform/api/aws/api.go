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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/aws")

type Options struct {
	*platform.Options
	// The AWS region regional api calls should use
	Region string
	// The profile to use when resolving credentials, if applicable
	Profile string

	// AccessKeyID is the optional access key to use. It will override all other sources
	AccessKeyID string
	// SecretKey is the optional secret key to use. It will override all other sources
	SecretKey string

	AMI           string
	InstanceType  string
	SecurityGroup string
}

type API struct {
	session client.ConfigProvider
	ec2     *ec2.EC2
	s3      *s3.S3
	opts    *Options
}

// New creates a new AWS API wrapper. It uses credentials from any of the
// standard credentials sources, including the environment and the profile
// configured in ~/.aws.
// No validation is done that credentials exist and before using the API a
// preflight check is recommended via api.PreflightCheck
func New(opts *Options) (*API, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		Profile: opts.Profile,
		Config:  aws.Config{Region: aws.String(opts.Region)},
	})
	if err != nil {
		return nil, err
	}

	if opts.AccessKeyID != "" {
		sess.Config.WithCredentials(credentials.NewStaticCredentials(opts.AccessKeyID, opts.SecretKey, ""))
	}

	api := &API{
		session: sess,
		ec2:     ec2.New(sess),
		s3:      s3.New(sess),
		opts:    opts,
	}

	return api, nil
}

// PreflightCheck validates that the aws configuration provided has valid
// credentials
func (a *API) PreflightCheck() error {
	stsClient := sts.New(a.session)
	_, err := stsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})

	return err
}
