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
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
)

func init() {
	build := sdk.BuildRoot()
	AWS.AddCommand(cmdUpload)
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "s3://bucket/prefix/; bucket defaults to a regional bucket and prefix defaults to $USER")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name for uploaded image, defaults to COREOS_VERSION")
	cmdUpload.Flags().StringVar(&uploadBoard, "board", "amd64-usr", "board used for naming with default prefix only")
	cmdUpload.Flags().StringVar(&uploadFile, "file",
		build+"/images/amd64-usr/latest/coreos_production_ami_vmdk_image.vmdk",
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=ami_vmdk ...)")
	cmdUpload.Flags().BoolVar(&uploadExpire, "expire", true, "expire the S3 image in 10 days")
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite existing S3 and AWS images without prompt")
}

func defaultBucket(in string, region string) (string, error) {
	if in == "" {
		in = fmt.Sprintf("s3://s3-%s.users.developer.core-os.net", region)
	}

	s3URL, err := url.Parse(in)
	if err != nil {
		return "", err
	}
	if s3URL.Scheme != "s3" {
		return "", fmt.Errorf("invalid s3 scheme; must be 's3://', not '%s://'", s3URL.Scheme)
	}
	if s3URL.Host == "" {
		return "", fmt.Errorf("URL missing bucket name %v\n", in)
	}

	// if prefix not specified default name to s3://bucket/$USER/$BOARD/$VERSION
	if s3URL.Path == "" {
		if user := os.Getenv("USER"); user != "" {
			s3URL.Path = "/" + os.Getenv("USER")
			s3URL.Path += "/" + uploadBoard
		}
	}

	return s3URL.String(), nil
}

func runUpload(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in aws upload cmd: %v\n", args)
		os.Exit(2)
	}

	s3Bucket, err := defaultBucket(uploadBucket, region)
	if err != nil {
		return fmt.Errorf("invalid bucket: %v", err)
	}

	// if an image name is unspecified try to use version.txt
	if uploadImageName == "" {
		ver, err := sdk.VersionsFromDir(filepath.Dir(uploadFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory: %v\n", err)
			os.Exit(1)
		}
		uploadImageName = ver.Version
	}

	plog.Debugf("upload bucket: %v\n", s3Bucket)
	s3URL, err := url.Parse(s3Bucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	plog.Debugf("parsed s3 url: %+v", s3URL)
	s3BucketName := s3URL.Host
	uploadImageName = strings.TrimPrefix(s3URL.Path+"/"+uploadImageName, "/")

	f, err := os.Open(uploadFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open image file %v: %v\n", uploadFile, err)
		os.Exit(1)
	}

	err = API.UploadImage(f, s3BucketName, uploadImageName, uploadExpire, uploadForce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created aws s3 upload: s3://%v/%v\n", s3BucketName, uploadImageName)
	return nil
}
