// Copyright 2018 Red Hat Inc.
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
	"fmt"

	"github.com/spf13/pflag"
)

var (
	specComposeID   string
	specEnv         string
	specRespin      string
	specImageType   string
	specTimestamp   string
	awsFedoraArches = []string{
		"x86_64",
		"aarch64",
	}
	awsFedoraProdAccountPartitions = []awsPartitionSpec{
		awsPartitionSpec{
			Name:         "AWS",
			Profile:      "default",
			Bucket:       "fedora-cloud-plume-ami-vmimport",
			BucketRegion: "us-east-1",
			LaunchPermissions: []string{
				"125523088429", // fedora production account
			},
			Regions: []string{
				"ap-northeast-2",
				"us-east-2",
				"ap-southeast-1",
				"ap-southeast-2",
				"ap-south-1",
				"eu-west-1",
				"sa-east-1",
				"us-east-1",
				"us-west-2",
				"us-west-1",
				"eu-central-1",
				"ap-northeast-1",
				"ca-central-1",
				"eu-west-2",
				"eu-west-3",
			},
		},
	}
	awsFedoraDevAccountPartitions = []awsPartitionSpec{
		awsPartitionSpec{
			Name:         "AWS",
			Profile:      "default",
			Bucket:       "prod-account-match-fedora-cloud-plume-ami-vmimport",
			BucketRegion: "us-east-1",
			LaunchPermissions: []string{
				"013116697141", // fedora community dev test account
			},
			Regions: []string{
				"us-east-2",
				"us-east-1",
			},
		},
	}

	fedoraSpecs = map[string]channelSpec{
		"rawhide": channelSpec{
			BaseURL: "https://koji.fedoraproject.org/compose/rawhide",
			Arches:  awsFedoraArches,
			AWS: awsSpec{
				BaseName:        "Fedora",
				BaseDescription: "Fedora Cloud Base AMI",
				Image:           "Fedora-{{.ImageType}}-{{.Version}}-{{.Timestamp}}.n.{{.Respin}}.{{.Arch}}.raw.xz",
				Partitions:      awsFedoraProdAccountPartitions,
			},
		},
		"branched": channelSpec{
			BaseURL: "https://koji.fedoraproject.org/compose/branched",
			Arches:  awsFedoraArches,
			AWS: awsSpec{
				BaseName:        "Fedora",
				BaseDescription: "Fedora Cloud Base AMI",
				Image:           "Fedora-{{.ImageType}}-{{.Version}}-{{.Timestamp}}.n.{{.Respin}}.{{.Arch}}.raw.xz",
				Partitions:      awsFedoraProdAccountPartitions,
			},
		},
		"updates": channelSpec{
			BaseURL: "https://koji.fedoraproject.org/compose/updates",
			Arches:  awsFedoraArches,
			AWS: awsSpec{
				BaseName:        "Fedora",
				BaseDescription: "Fedora Cloud Base AMI",
				Image:           "Fedora-{{.ImageType}}-{{.Version}}-{{.Timestamp}}.{{.Respin}}.{{.Arch}}.raw.xz",
				Partitions:      awsFedoraProdAccountPartitions,
			},
		},
		"cloud": channelSpec{
			BaseURL: "https://koji.fedoraproject.org/compose/cloud",
			Arches:  awsFedoraArches,
			AWS: awsSpec{
				BaseName:        "Fedora",
				BaseDescription: "Fedora Cloud Base AMI",
				Image:           "Fedora-{{.ImageType}}-{{.Version}}-{{.Timestamp}}.{{.Respin}}.{{.Arch}}.raw.xz",
				Partitions:      awsFedoraProdAccountPartitions,
			},
		},
	}
)

func AddFedoraSpecFlags(flags *pflag.FlagSet) {
	flags.StringVar(&specEnv, "environment", "prod", "AMI upload environment")
	flags.StringVar(&specImageType, "image-type", "Cloud-Base", "type of image")
	flags.StringVar(&specTimestamp, "timestamp", "", "compose timestamp")
	flags.StringVar(&specRespin, "respin", "0", "compose respin")
	flags.StringVar(&specComposeID, "compose-id", "", "compose id")
}

func ChannelFedoraSpec() (channelSpec, error) {
	if specComposeID == "" {
		plog.Fatal("--compose-id is required")
	}
	if specTimestamp == "" {
		plog.Fatal("--timestamp is required")
	}
	if specVersion == "" {
		plog.Fatal("--version is required")
	}
	if specArch == "" {
		specArch = "x86_64"
	}

	spec, ok := fedoraSpecs[specChannel]
	if !ok {
		return channelSpec{}, fmt.Errorf("Unknown channel: %q", specChannel)
	}

	if specEnv == "dev" {
		spec.AWS.Partitions = awsFedoraDevAccountPartitions
	}
	archOk := false
	for _, arch := range spec.Arches {
		if specArch == arch {
			archOk = true
			break
		}
	}
	if !archOk {
		plog.Fatalf("Unknown arch %q for channel %q", specArch, specChannel)
	}

	return spec, nil
}
