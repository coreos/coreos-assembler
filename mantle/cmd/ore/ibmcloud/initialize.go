// Copyright 2021 Red Hat
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

// IBMCloud has cloud-object-storage instances inside which buckets can be created in different regions: https://cloud.ibm.com/docs/cloud-object-storage

package ibmcloud

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	cmdInitialize = &cobra.Command{
		Use:   "initialize",
		Short: "initialize any uncreated resources for a given IBM Cloud region",
		RunE:  runInitialize,

		SilenceUsage: true,
	}

	cloudObjectStorage string
	resourceGroup      string
	bucket             string
)

func defaultBucketNameForRegion(region string) string {
	return fmt.Sprintf("coreos-dev-image-ibmcloud-%s", region)
}

func init() {
	defaultCloudObjectStorageName := "coreos-dev-image-ibmcloud"
	defaultCloudObjectStorageResourceGroup := "coreos"

	IbmCloud.AddCommand(cmdInitialize)
	cmdInitialize.Flags().StringVar(&cloudObjectStorage, "cloud-object-storage", defaultCloudObjectStorageName, "IBMCloud cloud object storage")
	cmdInitialize.Flags().StringVar(&resourceGroup, "resource-group", defaultCloudObjectStorageResourceGroup, "IBMCloud resource group the user belongs to")
	cmdInitialize.Flags().StringVar(&bucket, "bucket", "", "the S3 bucket to initialize; will default to a regional bucket")
}

func runInitialize(cmd *cobra.Command, args []string) error {
	if bucket == "" {
		bucket = defaultBucketNameForRegion(region)
	}

	// check if the cloud-object-storage exists, if not create it
	instances, err := API.ListCloudObjectStorageInstances()
	if err != nil {
		return err
	}

	if _, ok := instances[cloudObjectStorage]; !ok {
		_, err := API.CreateCloudObjectStorageInstance(cloudObjectStorage, resourceGroup)
		if err != nil {
			return err
		}
	}

	// check if the s3 bucket exists, if not create it
	// create s3 client
	err = API.NewS3Client(cloudObjectStorage, region)
	if err != nil {
		return err
	}

	bucketExists, err := API.CheckBucketExists(bucket)
	if err != nil {
		return err
	}

	if !bucketExists {
		err = API.CreateBucket(bucket)
		if err != nil {
			return err
		}
	}

	return nil
}
