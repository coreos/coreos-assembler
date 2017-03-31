// Copyright 2017 CoreOS, Inc.
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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/sdk"
	"github.com/spf13/cobra"
)

var (
	cmdUpload = &cobra.Command{
		Use:   "upload",
		Short: "Upload an AMI to s3",
		Long:  "Upload a streaming vmdk to s3 for use by create-snapshot",
		RunE:  runUpload,
	}

	uploadBucket    string
	uploadImageName string
	uploadBoard     string
	uploadFile      string
	uploadExpire    bool
	uploadForce     bool

	cmdCreateImages = &cobra.Command{
		Use:   "create-images",
		Short: "Create AWS images",
		Long: `Create AWS images. This will create all relevant AMIs (hvm, pv, etc).

Supported source formats are VMDK (as created with ./image_to_vm --format=ami_vmdk) and RAW.

The image may be uploaded to S3 manually, or with the 'ore aws upload' command.

The flags allow controlling various knobs about the images.

After a successful run, the final line of output will be a line of JSON describing the image resources created and the underlying snapshots

A common usage is:

    ore aws create-images --region=us-west-2 \
		  --snapshot-description="CoreOS-stable-1234.5.6" \
		  --name="CoreOS-stable-1234.5.6" \
		  --description="CoreOS stable 1234.5.6" \
		  --snapshot-source "s3://s3-us-west-2.users.developer.core-os.net/.../coreos_production_ami_vmdk_image.vmdk"
`,
		RunE: runCreateImages,
	}

	name                string
	description         string
	createPV            bool
	snapshotID          string
	snapshotSource      string
	snapshotDescription string
	format              aws.EC2ImageFormat
)

func init() {
	uploadBoard = "amd64-usr"

	AWS.AddCommand(cmdUpload)
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "s3://bucket/prefix/; defaults to a regional bucket and prefix defaults to $USER")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name for uploaded image, defaults to COREOS_VERSION")
	cmdUpload.Flags().StringVar(&uploadBoard, "board", "amd64-usr", "board used for naming with default prefix only")
	cmdUpload.Flags().StringVar(&uploadFile, "file",
		defaultUploadFile(),
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=ami_vmdk ...)")
	cmdUpload.Flags().BoolVar(&uploadExpire, "expire", true, "expire the S3 image in 10 days")
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite existing S3 and AWS images without prompt")

	AWS.AddCommand(cmdCreateImages)
	cmdCreateImages.Flags().StringVar(&name, "name", "", "the name of the image to create; defaults to Container-Linux-$USER-$VERSION")
	cmdCreateImages.Flags().StringVar(&description, "description", "", "the description of the image to create")
	cmdCreateImages.Flags().BoolVar(&createPV, "create-pv", true, "whether to create a PV AMI in addition the the HVM AMI")
	cmdCreateImages.Flags().StringVar(&snapshotID, "snapshot-id", "", "[optional] the snapshot ID to base this AMI off of. A new snapshot will be created if not provided.")
	cmdCreateImages.Flags().StringVar(&snapshotSource, "snapshot-source", "", "snapshot source: must be an 's3://' URI; defaults to the same as upload if unset")
	cmdCreateImages.Flags().StringVar(&snapshotDescription, "snapshot-description", "", "snapshot description")
	cmdCreateImages.Flags().Var(&format, "snapshot-format", fmt.Sprintf("snapshot format: default %s, %s or %s", aws.EC2ImageFormatVmdk, aws.EC2ImageFormatVmdk, aws.EC2ImageFormatRaw))
}

func defaultBucketNameForRegion(region string) string {
	return fmt.Sprintf("s3-%s.users.developer.core-os.net", region)
}

func defaultUploadFile() string {
	build := sdk.BuildRoot()
	return build + "/images/amd64-usr/latest/coreos_production_ami_vmdk_image.vmdk"
}

// defaultBucketURI determines the location the tool should upload to.
// The 's3URI' parameter, if it contains a path, will override all other
// arguments
func defaultBucketURI(s3URI, imageName, board, file, region string) (string, error) {
	if s3URI == "" {
		s3URI = fmt.Sprintf("s3://%s", defaultBucketNameForRegion(region))
	}

	s3URL, err := url.Parse(s3URI)
	if err != nil {
		return "", err
	}
	if s3URL.Scheme != "s3" {
		return "", fmt.Errorf("invalid s3 scheme; must be 's3://', not '%s://'", s3URL.Scheme)
	}
	if s3URL.Host == "" {
		return "", fmt.Errorf("URL missing bucket name %v\n", s3URI)
	}

	// if prefix not specified default name to s3://bucket/$USER/$BOARD/$VERSION
	if s3URL.Path == "" {
		if board == "" {
			board = "amd64-usr"
		}

		user := os.Getenv("USER")

		s3URL.Path = "/" + os.Getenv("USER")
		s3URL.Path += "/" + board

		if file == "" {
			file = defaultUploadFile()
		}

		// if an image name is unspecified try to use version.txt
		if imageName == "" {
			ver, err := sdk.VersionsFromDir(filepath.Dir(file))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory: %v\n", err)
				os.Exit(1)
			}
			imageName = ver.Version
		}
		fileName := filepath.Base(file)

		s3URL.Path = fmt.Sprintf("/%s/%s/%s/%s", user, board, imageName, fileName)
	}

	return s3URL.String(), nil
}

func createSnapshot() (string, error) {
	snapshotSource, err := defaultBucketURI(snapshotSource, "", "", "", region)

	if err != nil {
		return "", fmt.Errorf("unable to guess snapshot source: %v", err)
	}
	snapshot, err := API.CreateSnapshot(snapshotDescription, snapshotSource, format)
	if err != nil {
		return "", fmt.Errorf("unable to create snapshot: %v", err)
	}

	return snapshot.SnapshotID, nil
}

func runUpload(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in aws upload cmd: %v\n", args)
		os.Exit(2)
	}

	s3Bucket, err := defaultBucketURI(uploadBucket, uploadImageName, uploadBoard, uploadFile, region)
	if err != nil {
		return fmt.Errorf("invalid bucket: %v", err)
	}
	if uploadFile == "" {
		uploadFile = defaultUploadFile()
	}

	plog.Debugf("upload bucket: %v\n", s3Bucket)
	s3URL, err := url.Parse(s3Bucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	plog.Debugf("parsed s3 url: %+v", s3URL)
	s3BucketName := s3URL.Host
	s3BucketPath := strings.TrimPrefix(s3URL.Path, "/")

	f, err := os.Open(uploadFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open image file %v: %v\n", uploadFile, err)
		os.Exit(1)
	}

	err = API.UploadObject(f, s3BucketName, s3BucketPath, uploadExpire, uploadForce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created aws s3 upload: s3://%v/%v\n", s3BucketName, s3BucketPath)
	return nil
}

func runCreateImages(cmd *cobra.Command, args []string) error {
	if name == "" {
		buildDir := sdk.BuildRoot() + "/images/amd64-usr/latest/coreos_production_ami_vmdk_image.vmdk"
		ver, err := sdk.VersionsFromDir(filepath.Dir(buildDir))
		if err != nil {
			return fmt.Errorf("could not guess image name: %v", err)
		}
		awsVersion := strings.Replace(ver.Version, "+", "-", -1) // '+' is invalid in an AMI name
		name = fmt.Sprintf("Container-Linux-dev-%s-%s", os.Getenv("USER"), awsVersion)
	}

	if snapshotID == "" {
		newSnapshotID, err := createSnapshot()
		if err != nil {
			return fmt.Errorf("unable to create snapshot: %v", err)
		}
		snapshotID = newSnapshotID
	}

	hvmID, err := API.CreateHVMImage(snapshotID, name, description)
	if err != nil {
		return fmt.Errorf("unable to create HVM image: %v", err)
	}
	var pvID string
	if createPV {
		pvImageID, err := API.CreatePVImage(snapshotID, name, description)
		if err != nil {
			return fmt.Errorf("unable to create PV image: %v", err)
		}
		pvID = pvImageID
	}

	json.NewEncoder(os.Stdout).Encode(&struct {
		HVM        string
		PV         string `json:",omitempty"`
		SnapshotID string
	}{
		HVM:        hvmID,
		PV:         pvID,
		SnapshotID: snapshotID,
	})
	return nil
}
