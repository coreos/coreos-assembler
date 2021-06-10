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

package ibmcloud

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdUpload = &cobra.Command{
		Use:   "upload",
		Short: "Create IBMCloud images",
		Long: `Upload CoreOS image to IBMCloud cloud object storage S3 bucket.

Supported source format is qcow.
`,
		Example: `  ore ibmcloud upload --region=us-east \
	  --cloud-object-storage=coreos-dev-image-ibmcloud \
	  --bucket=coreos-dev-image-ibmcloud-us-east
	  --file="/home/.../coreos_production_qcow_image.qcow2"`,
		RunE: runUpload,

		SilenceUsage: true,
	}

	uploadCloudObjectStorage string
	uploadBucket             string
	uploadImageName          string
	uploadFile               string
	uploadForce              bool
)

func init() {
	IbmCloud.AddCommand(cmdUpload)

	cmdUpload.Flags().StringVar(&uploadCloudObjectStorage, "cloud-object-storage", cloudObjectStorage, "IBMCloud cloud object storage")
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "defaults to a regional bucket")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name of uploaded image")
	cmdUpload.Flags().StringVar(&uploadFile, "file", "", "path to CoreOS image")
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite any existing S3 object, snapshot, and AMI")
}

func runUpload(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in ibmcloud upload cmd: %v\n", args)
		os.Exit(2)
	}
	if uploadFile == "" {
		fmt.Fprintf(os.Stderr, "specify --file\n")
		os.Exit(2)
	}
	if uploadImageName == "" {
		fmt.Fprintf(os.Stderr, "unknown image name; specify --name\n")
		os.Exit(2)
	}

	if uploadBucket == "" {
		uploadBucket = defaultBucketNameForRegion(region)
	}

	// check if the cloud-object-storage exists
	instances, err := API.ListCloudObjectStorageInstances()
	if err != nil {
		return err
	}

	if _, ok := instances[uploadCloudObjectStorage]; !ok {
		fmt.Fprintf(os.Stderr, "IBMCloud cloud object storage does not exist. Create using the initialize command\n")
		os.Exit(2)
	}

	// check if the s3 bucket exists
	// create s3 client
	if err := API.NewS3Client(uploadCloudObjectStorage, region); err != nil {
		return err
	}

	bucketExists, err := API.CheckBucketExists(uploadBucket)
	if err != nil {
		return err
	}

	if !bucketExists {
		fmt.Fprintf(os.Stderr, "the specified bucket in the cloud object storage does not exist. Create using the initialize command\n")
		os.Exit(2)
	}

	f, err := os.Open(uploadFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open image file %v: %v\n", uploadFile, err)
		os.Exit(1)
	}
	defer f.Close()

	err = API.UploadObject(f, uploadImageName, uploadBucket, uploadForce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading: %v\n", err)
		os.Exit(1)
	}

	return nil
}
