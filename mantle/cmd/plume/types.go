// Copyright 2016-2018 Red Hat Inc.
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

type storageSpec struct {
	BaseURL       string
	Title         string // Replace the bucket name in index page titles
	NamedPath     string // Copy to $BaseURL/$Arch/$NamedPath
	VersionPath   bool   // Copy to $BaseURL/$Arch/$Version
	DirectoryHTML bool
	IndexHTML     bool
}

type gceSpec struct {
	Project     string   // GCE project name
	Family      string   // A group name, also used as name prefix
	Description string   // Human readable-ish description
	Licenses    []string // Identifiers for tracking usage
	Image       string   // File name of image source
	Publish     string   // Write published image name to given file
	Limit       int      // Limit on # of old images to keep
}

type azureEnvironmentSpec struct {
	SubscriptionName     string   // Name of subscription in Azure profile
	AdditionalContainers []string // Extra containers to upload the disk image to
}

type azureSpec struct {
	Offer          string                 // Azure offer name
	Image          string                 // File name of image source
	StorageAccount string                 // Storage account to use for image uploads in each environment
	Container      string                 // Container to hold the disk image in each environment
	Environments   []azureEnvironmentSpec // Azure environments to upload to

	// Fields for azure.OSImage
	Label             string
	Description       string // Description of an image in this channel
	RecommendedVMSize string
	IconURI           string
	SmallIconURI      string
}

type awsPartitionSpec struct {
	Name              string   // Printable name for the partition
	Profile           string   // Authentication profile in ~/.aws
	Bucket            string   // S3 bucket for uploading image
	BucketRegion      string   // Region of the bucket
	LaunchPermissions []string // Other accounts to give launch permission
	Regions           []string // Regions to create the AMI in
}

type awsSpec struct {
	BaseName        string             // Prefix of image name
	BaseDescription string             // Prefix of image description
	Prefix          string             // Prefix for filenames of AMI lists
	Image           string             // File name of image source
	Partitions      []awsPartitionSpec // AWS partitions
}

type channelSpec struct {
	BaseURL      string // Copy from $BaseURL/$Arch/$Version
	Arches       []string
	Destinations []storageSpec
	GCE          gceSpec
	Azure        azureSpec
	AWS          awsSpec
}

type fcosChannelSpec struct {
	Bucket  string
	Profile string
	Region  string
}
