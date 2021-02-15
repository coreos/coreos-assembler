// Copyright 2019 Red Hat Inc.
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

package main

import (
	"github.com/spf13/pflag"
)

var (
	specBucket   string
	specRegion   string
	specProfile  string
	specPolicy   string
	specCommitId string
	specArch     string
	specChannel  string
	specVersion  string
)

func AddFcosSpecFlags(flags *pflag.FlagSet) {
	flags.StringVar(&specBucket, "bucket", "fcos-builds", "S3 bucket")
	flags.StringVar(&specRegion, "region", "us-east-1", "S3 bucket region")
	flags.StringVar(&specProfile, "profile", "default", "AWS profile")
	flags.StringVar(&specPolicy, "policy", "public-read", "Canned ACL policy")
}

func FcosValidateArguments() {
	if specVersion == "" {
		plog.Fatal("--version is required")
	}
	if specChannel == "" {
		plog.Fatal("--channel is required")
	}
	if specBucket == "" {
		plog.Fatal("--bucket is required")
	}
	if specRegion == "" {
		plog.Fatal("--region is required")
	}
}

func FcosChannelSpec() fcosChannelSpec {
	return fcosChannelSpec{
		Bucket:  specBucket,
		Profile: specProfile,
		Region:  specRegion,
	}
}
