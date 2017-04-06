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
		Short: "Create AWS images",
		Long: `Upload CoreOS image to S3 and create relevant AMIs (hvm and pv).

Supported source formats are VMDK (as created with ./image_to_vm --format=ami_vmdk) and RAW.

After a successful run, the final line of output will be a line of JSON describing the relevant resources.
`,
		Example: `  ore aws upload --region=us-west-2 \
	  --ami-name="CoreOS-stable-1234.5.6" \
	  --ami-description="CoreOS stable 1234.5.6" \
	  --file="/home/.../coreos_production_ami_vmdk_image.vmdk"`,
		RunE: runUpload,
	}

	uploadSourceObject   string
	uploadBucket         string
	uploadImageName      string
	uploadBoard          string
	uploadFile           string
	uploadDeleteObject   bool
	uploadForce          bool
	uploadSourceSnapshot string
	uploadObjectFormat   aws.EC2ImageFormat
	uploadAMIName        string
	uploadAMIDescription string
	uploadGrantUsers     []string
	uploadCreatePV       bool
)

func init() {
	AWS.AddCommand(cmdUpload)
	cmdUpload.Flags().StringVar(&uploadSourceObject, "source-object", "", "'s3://' URI pointing to image data (default: same as upload)")
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "s3://bucket/prefix/ (defaults to a regional bucket and prefix defaults to $USER)")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name of uploaded image (default COREOS_VERSION)")
	cmdUpload.Flags().StringVar(&uploadBoard, "board", "amd64-usr", "board used for naming with default prefix only")
	cmdUpload.Flags().StringVar(&uploadFile, "file",
		defaultUploadFile(),
		"path to CoreOS image (build with: ./image_to_vm.sh --format=ami_vmdk ...)")
	cmdUpload.Flags().BoolVar(&uploadDeleteObject, "delete-object", true, "delete uploaded S3 object after snapshot is created")
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite existing S3 object without prompt")
	cmdUpload.Flags().StringVar(&uploadSourceSnapshot, "source-snapshot", "", "the snapshot ID to base this AMI on (default: create new snapshot)")
	cmdUpload.Flags().Var(&uploadObjectFormat, "object-format", fmt.Sprintf("object format: %s or %s (default: %s)", aws.EC2ImageFormatVmdk, aws.EC2ImageFormatRaw, aws.EC2ImageFormatVmdk))
	cmdUpload.Flags().StringVar(&uploadAMIName, "ami-name", "", "name of the AMI to create (default: Container-Linux-$USER-$VERSION)")
	cmdUpload.Flags().StringVar(&uploadAMIDescription, "ami-description", "", "description of the AMI to create (default: empty)")
	cmdUpload.Flags().StringSliceVar(&uploadGrantUsers, "grant-user", []string{}, "grant launch permission to this AWS user ID")
	cmdUpload.Flags().BoolVar(&uploadCreatePV, "create-pv", true, "create a PV AMI in addition to the HVM AMI")
}

func defaultBucketNameForRegion(region string) string {
	return fmt.Sprintf("s3-%s.users.developer.core-os.net", region)
}

func defaultUploadFile() string {
	build := sdk.BuildRoot()
	return build + "/images/amd64-usr/latest/coreos_production_ami_vmdk_image.vmdk"
}

// defaultBucketURL determines the location the tool should upload to.
// The 'urlPrefix' parameter, if it contains a path, will override all other
// arguments
func defaultBucketURL(urlPrefix, imageName, board, file, region string) (*url.URL, error) {
	if urlPrefix == "" {
		urlPrefix = fmt.Sprintf("s3://%s", defaultBucketNameForRegion(region))
	}

	s3URL, err := url.Parse(urlPrefix)
	if err != nil {
		return nil, err
	}
	if s3URL.Scheme != "s3" {
		return nil, fmt.Errorf("invalid s3 scheme; must be 's3://', not '%s://'", s3URL.Scheme)
	}
	if s3URL.Host == "" {
		return nil, fmt.Errorf("URL missing bucket name %v\n", urlPrefix)
	}

	// if prefix not specified default name to s3://bucket/$USER/$BOARD/$VERSION
	if s3URL.Path == "" {
		user := os.Getenv("USER")

		s3URL.Path = "/" + os.Getenv("USER")
		s3URL.Path += "/" + board

		fileName := filepath.Base(file)

		s3URL.Path = fmt.Sprintf("/%s/%s/%s/%s", user, board, imageName, fileName)
	}

	return s3URL, nil
}

func runUpload(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in aws upload cmd: %v\n", args)
		os.Exit(2)
	}
	if uploadSourceObject != "" && uploadSourceSnapshot != "" {
		fmt.Fprintf(os.Stderr, "At most one of --source-object and --source-snapshot may be specified.\n")
		os.Exit(2)
	}

	// if an image name is unspecified try to use version.txt
	imageName := uploadImageName
	if imageName == "" {
		ver, err := sdk.VersionsFromDir(filepath.Dir(uploadFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory: %v\n", err)
			os.Exit(1)
		}
		imageName = ver.Version
	}

	amiName := uploadAMIName
	if amiName == "" {
		ver, err := sdk.VersionsFromDir(filepath.Dir(uploadFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not guess image name: %v\n", err)
			os.Exit(1)
		}
		awsVersion := strings.Replace(ver.Version, "+", "-", -1) // '+' is invalid in an AMI name
		amiName = fmt.Sprintf("Container-Linux-dev-%s-%s", os.Getenv("USER"), awsVersion)
	}

	var s3URL *url.URL
	var err error
	if uploadSourceObject != "" {
		s3URL, err = url.Parse(uploadSourceObject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	} else {
		s3URL, err = defaultBucketURL(uploadBucket, imageName, uploadBoard, uploadFile, region)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}
	plog.Debugf("S3 object: %v\n", s3URL)
	s3BucketName := s3URL.Host
	s3ObjectPath := strings.TrimPrefix(s3URL.Path, "/")

	// if no snapshot was specified, check for an existing one or a
	// snapshot task in progress
	sourceSnapshot := uploadSourceSnapshot
	if sourceSnapshot == "" {
		snapshot, err := API.FindSnapshot(imageName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed finding snapshot: %v\n", err)
			os.Exit(1)
		}
		if snapshot != nil {
			sourceSnapshot = snapshot.SnapshotID
		}
	}

	// if there's no existing snapshot and no provided S3 object to
	// make one from, upload to S3
	if uploadSourceObject == "" && sourceSnapshot == "" {
		f, err := os.Open(uploadFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not open image file %v: %v\n", uploadFile, err)
			os.Exit(1)
		}
		defer f.Close()

		err = API.UploadObject(f, s3BucketName, s3ObjectPath, uploadForce)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error uploading: %v\n", err)
			os.Exit(1)
		}
	}

	// if we don't already have a snapshot, make one
	if sourceSnapshot == "" {
		snapshot, err := API.CreateSnapshot(imageName, s3URL.String(), uploadObjectFormat)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create snapshot: %v\n", err)
			os.Exit(1)
		}
		sourceSnapshot = snapshot.SnapshotID
	}

	// if delete is enabled and we created the snapshot from an S3
	// object that we also created (perhaps in a previous run), delete
	// the S3 object
	if uploadSourceObject == "" && uploadSourceSnapshot == "" && uploadDeleteObject {
		if err := API.DeleteObject(s3BucketName, s3ObjectPath); err != nil {
			fmt.Fprintf(os.Stderr, "unable to delete object: %v\n", err)
			os.Exit(1)
		}
	}

	// create AMIs and grant permissions
	hvmID, err := API.CreateHVMImage(sourceSnapshot, amiName+"-hvm", uploadAMIDescription)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create HVM image: %v\n", err)
		os.Exit(1)
	}

	if len(uploadGrantUsers) > 0 {
		err = API.GrantLaunchPermission(hvmID, uploadGrantUsers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to grant launch permission: %v\n", err)
			os.Exit(1)
		}
	}

	var pvID string
	if uploadCreatePV {
		pvImageID, err := API.CreatePVImage(sourceSnapshot, amiName, uploadAMIDescription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create PV image: %v\n", err)
			os.Exit(1)
		}
		pvID = pvImageID

		if len(uploadGrantUsers) > 0 {
			err = API.GrantLaunchPermission(pvID, uploadGrantUsers)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to grant launch permission: %v\n", err)
				os.Exit(1)
			}
		}
	}

	err = json.NewEncoder(os.Stdout).Encode(&struct {
		HVM        string
		PV         string `json:",omitempty"`
		SnapshotID string
		S3Object   string
	}{
		HVM:        hvmID,
		PV:         pvID,
		SnapshotID: sourceSnapshot,
		S3Object:   s3URL.String(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", err)
		os.Exit(1)
	}
	return nil
}
