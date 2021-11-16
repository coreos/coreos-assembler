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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
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

	// The path to the shared credentials file, if not ~/.aws/credentials
	CredentialsFile string
	// The profile to use when resolving credentials, if applicable
	Profile string

	// AccessKeyID is the optional access key to use. It will override all other sources
	AccessKeyID string
	// SecretKey is the optional secret key to use. It will override all other sources
	SecretKey string

	// AMI is the AWS AMI to launch EC2 instances with.
	// If it is one of the special strings alpha|beta|stable, it will be resolved
	// to an actual ID.
	AMI                string
	InstanceType       string
	SecurityGroup      string
	IAMInstanceProfile string
}

type API struct {
	session client.ConfigProvider
	ec2     *ec2.EC2
	iam     *iam.IAM
	s3      *s3.S3
	opts    *Options
}

// New creates a new AWS API wrapper. It uses credentials from any of the
// standard credentials sources, including the environment and the profile
// configured in ~/.aws.
// No validation is done that credentials exist and before using the API a
// preflight check is recommended via api.PreflightCheck
// Note that this method may modify Options to update the AMI ID
func New(opts *Options) (*API, error) {
	awsCfg := aws.Config{Region: aws.String(opts.Region)}
	if opts.AccessKeyID != "" {
		awsCfg.Credentials = credentials.NewStaticCredentials(opts.AccessKeyID, opts.SecretKey, "")
	} else if opts.CredentialsFile != "" {
		awsCfg.Credentials = credentials.NewSharedCredentials(opts.CredentialsFile, opts.Profile)
	}

	sess, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Profile:           opts.Profile,
		Config:            awsCfg,
	})
	if err != nil {
		return nil, err
	}

	opts.AMI = resolveAMI(opts.AMI, opts.Region)

	api := &API{
		session: sess,
		ec2:     ec2.New(sess),
		iam:     iam.New(sess),
		s3:      s3.New(sess),
		opts:    opts,
	}

	return api, nil
}

// GC removes AWS resources that are at least gracePeriod old.
// It attempts to only operate on resources that were created by a mantle tool.
func (a *API) GC(gracePeriod time.Duration) error {
	return a.gcEC2(gracePeriod)
}

// PreflightCheck validates that the aws configuration provided has valid
// credentials
func (a *API) PreflightCheck() error {
	stsClient := sts.New(a.session)
	_, err := stsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})

	return err
}

func tagSpecCreatedByMantle(resourceType string) []*ec2.TagSpecification {
	return []*ec2.TagSpecification{
		{
			ResourceType: aws.String(resourceType),
			Tags: []*ec2.Tag{
				&ec2.Tag{
					Key:   aws.String("CreatedBy"),
					Value: aws.String("mantle"),
				},
			},
		},
	}
}
