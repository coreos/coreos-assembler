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
	BaseURL string // Copy from $BaseURL/$Arch/$Version
	Arches  []string
	AWS     awsSpec
}

type fcosChannelSpec struct {
	Bucket  string
	Profile string
	Region  string
}
