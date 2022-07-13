// Copyright 2021 RedHat.
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

package ibmcloud

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdCopyObject = &cobra.Command{
		Use:   "copy-object",
		Short: "Copy IBMCloud object to a bucket",
		Long:  "Copy an IBMCloud object from a bucket in a region to another bucket in a specified region",
		Example: `  ore ibmcloud copy-object --source-name=image.qcow2 \
		--destination-region=us-south \
		--destination-bucket=coreos-dev-image-ibmcloud-us-south`,
		RunE: runCopyObject,

		SilenceUsage: true,
	}

	copyCloudObjectStorage string
	sourceBucket           string
	sourceName             string
	destRegion             string
	destBucket             string
)

func init() {
	IbmCloud.AddCommand(cmdCopyObject)
	cmdCopyObject.Flags().StringVar(&copyCloudObjectStorage, "cloud-object-storage", "coreos-dev-image-ibmcloud", "cloud object storage to be used")
	cmdCopyObject.Flags().StringVar(&sourceBucket, "source-bucket", "coreos-dev-image-ibmcloud-us-east", "bucket where object needs to be copied from")
	cmdCopyObject.Flags().StringVar(&sourceName, "source-name", "", "name of object to be copied")
	if err := cmdCopyObject.MarkFlagRequired("source-name"); err != nil {
		panic(err)
	}
	cmdCopyObject.Flags().StringVar(&destRegion, "destination-region", "", "region to be copied to")
	if err := cmdCopyObject.MarkFlagRequired("destination-region"); err != nil {
		panic(err)
	}
	cmdCopyObject.Flags().StringVar(&destBucket, "destination-bucket", "", "destination bucket to copy to")
	if err := cmdCopyObject.MarkFlagRequired("destination-bucket"); err != nil {
		panic(err)
	}
}

func runCopyObject(cmd *cobra.Command, args []string) error {
	if err := API.NewS3Client(copyCloudObjectStorage, destRegion); err != nil {
		return err
	}

	bucketExists, err := API.CheckBucketExists(destBucket)
	if err != nil {
		return err
	}

	if !bucketExists {
		if err = API.CreateBucket(destBucket); err != nil {
			return err
		}
	}

	if err = API.CopyObject(sourceBucket, sourceName, destBucket); err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't copy objects: %v\n", err)
		os.Exit(1)
	}

	return nil
}
