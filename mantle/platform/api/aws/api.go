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
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/platform"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/aws")

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
	config aws.Config
	ec2    *ec2.Client
	iam    *iam.Client
	s3     *s3.Client
	sts    *sts.Client
	opts   *Options
}

// New creates a new AWS API wrapper. It uses credentials from any of the
// standard credentials sources, including the environment and the profile
// configured in ~/.aws.
// No validation is done that credentials exist and before using the API a
// preflight check is recommended via api.PreflightCheck
// Note that this method may modify Options to update the AMI ID
func New(opts *Options) (*API, error) {
	ctx := context.Background()

	// Build configuration options
	configOpts := []func(*config.LoadOptions) error{
		config.WithRegion(opts.Region),
	}

	if opts.AccessKeyID != "" {
		configOpts = append(configOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretKey, ""),
		))
	} else if opts.CredentialsFile != "" {
		configOpts = append(configOpts, config.WithSharedCredentialsFiles([]string{opts.CredentialsFile}))
		if opts.Profile != "" {
			configOpts = append(configOpts, config.WithSharedConfigProfile(opts.Profile))
		}
	} else if opts.Profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(opts.Profile))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return nil, err
	}

	api := &API{
		config: awsCfg,
		ec2:    ec2.NewFromConfig(awsCfg),
		iam:    iam.NewFromConfig(awsCfg),
		s3:     s3.NewFromConfig(awsCfg),
		sts:    sts.NewFromConfig(awsCfg),
		opts:   opts,
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
	ctx := context.Background()
	_, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})

	return err
}

func tagSpecCreatedByMantle(name string, resourceType ec2types.ResourceType) []ec2types.TagSpecification {
	return []ec2types.TagSpecification{
		{
			ResourceType: resourceType,
			Tags: []ec2types.Tag{
				{
					Key:   aws.String("CreatedBy"),
					Value: aws.String("mantle"),
				},
				{
					Key:   aws.String("Name"),
					Value: aws.String(name),
				},
			},
		},
	}
}
