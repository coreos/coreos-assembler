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

	"github.com/coreos/coreos-assembler/mantle/platform/api/aws"
	"github.com/coreos/coreos-assembler/mantle/util"
	"github.com/spf13/cobra"
)

var (
	cmdUpload = &cobra.Command{
		Use:   "upload",
		Short: "Create AWS images",
		Long: `Upload CoreOS image to S3 and create an AMI.

Supported source formats are VMDK (as created with cosa buildextend-aws) and RAW.

After a successful run, the final line of output will be a line of JSON describing the relevant resources.
`,
		Example: `  ore aws upload --region=us-east-1 \
	  --ami-name="CoreOS-stable-1234.5.6" \
	  --ami-description="CoreOS stable 1234.5.6" \
	  --file="/home/.../coreos_production_ami_vmdk_image.vmdk" \
	  --tags="machine=production"`,
		RunE: runUpload,

		SilenceUsage: true,
	}

	uploadSourceObject       string
	uploadBucket             string
	uploadImageName          string
	uploadImageArchitecture  string
	uploadFile               string
	uploadDiskSizeGiB        uint
	uploadDiskSizeInspect    bool
	uploadDeleteObject       bool
	uploadForce              bool
	uploadSourceSnapshot     string
	uploadObjectFormat       aws.EC2ImageFormat
	uploadAMIName            string
	uploadAMIDescription     string
	uploadPublic             bool
	uploadGrantUsers         []string
	uploadGrantUsersSnapshot []string
	uploadTags               []string
	uploadIMDSv2Only         bool
	uploadVolumeType         string
	uploadX86BootMode        string
)

func init() {
	AWS.AddCommand(cmdUpload)
	cmdUpload.Flags().StringVar(&uploadSourceObject, "source-object", "", "'s3://' URI pointing to image data (default: same as upload)")
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "s3://bucket/prefix/ (defaults to a regional bucket and prefix defaults to $USER/board/name)")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name of uploaded image")
	cmdUpload.Flags().StringVar(&uploadImageArchitecture, "arch", "", "The target architecture for the AMI")
	cmdUpload.Flags().StringVar(&uploadFile, "file", "", "path to CoreOS image")
	cmdUpload.Flags().UintVarP(&uploadDiskSizeGiB, "disk-size-gib", "", aws.ContainerLinuxDiskSizeGiB, "AMI disk size in GiB")
	cmdUpload.Flags().BoolVar(&uploadDiskSizeInspect, "disk-size-inspect", false, "set AMI disk size to size of local file")
	cmdUpload.Flags().BoolVar(&uploadDeleteObject, "delete-object", true, "delete uploaded S3 object after snapshot is created")
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite any existing S3 object, snapshot, and AMI")
	cmdUpload.Flags().StringVar(&uploadSourceSnapshot, "source-snapshot", "", "the snapshot ID to base this AMI on (default: create new snapshot)")
	cmdUpload.Flags().Var(&uploadObjectFormat, "object-format", fmt.Sprintf("object format: %s or %s (default: %s)", aws.EC2ImageFormatVmdk, aws.EC2ImageFormatRaw, aws.EC2ImageFormatVmdk))
	cmdUpload.Flags().StringVar(&uploadAMIName, "ami-name", "", "name of the AMI to create")
	cmdUpload.Flags().StringVar(&uploadAMIDescription, "ami-description", "", "description of the AMI to create (default: empty)")
	cmdUpload.Flags().BoolVar(&uploadPublic, "public", false, "make image and snapshot volume public")
	cmdUpload.Flags().StringSliceVar(&uploadGrantUsers, "grant-user", []string{}, "grant launch permission to this AWS user ID")
	cmdUpload.Flags().StringSliceVar(&uploadGrantUsersSnapshot, "grant-user-snapshot", []string{}, "grant snapshot volume permission to this AWS user ID")
	cmdUpload.Flags().StringSliceVar(&uploadTags, "tags", []string{}, "list of key=value tags to attach to the AMI")
	cmdUpload.Flags().BoolVar(&uploadIMDSv2Only, "imdsv2-only", false, "enable IMDSv2-only support")
	cmdUpload.Flags().StringVar(&uploadVolumeType, "volume-type", "gp3", "EBS volume type (gp3, gp2, io1, st1, sc1, standard, etc.)")
	cmdUpload.Flags().StringVar(&uploadX86BootMode, "x86-boot-mode", "uefi-preferred", "Set boot mode (uefi-preferred, uefi)")
}

func defaultBucketNameForRegion(region string) string {
	return fmt.Sprintf("coreos-dev-ami-import-%s", region)
}

// defaultBucketURL determines the location the tool should upload to.
// The 'urlPrefix' parameter, if it contains a path, will override all other
// arguments
func defaultBucketURL(urlPrefix, file, region string) (*url.URL, error) {
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

	// if prefix not specified, default to 'ami-import'
	if s3URL.Path == "" {
		s3URL.Path = "/ami-import"
	}

	if s3URL.Path[len(s3URL.Path)-1] != '/' {
		s3URL.Path += "/"
	}
	s3URL.Path += filepath.Base(file)

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
	if uploadDiskSizeInspect && (uploadSourceObject != "" || uploadSourceSnapshot != "") {
		fmt.Fprintf(os.Stderr, "--disk-size-inspect cannot be used with --source-object or --source-snapshot.\n")
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
	if uploadAMIName == "" {
		fmt.Fprintf(os.Stderr, "unknown AMI name; specify --ami-name\n")
		os.Exit(2)
	}

	if uploadDiskSizeInspect {
		imageInfo, err := util.GetImageInfo(uploadFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to query size of disk: %v\n", err)
			os.Exit(1)
		}
		plog.Debugf("Image size: %v\n", imageInfo.VirtualSize)
		const GiB = 1024 * 1024 * 1024
		uploadDiskSizeGiB = uint(imageInfo.VirtualSize / GiB)
		// Round up if there's leftover
		if imageInfo.VirtualSize%GiB > 0 {
			uploadDiskSizeGiB += 1
		}
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
		s3URL, err = defaultBucketURL(uploadBucket, uploadFile, region)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}
	plog.Debugf("S3 object: %v\n", s3URL)
	s3BucketName := s3URL.Host
	s3ObjectPath := strings.TrimPrefix(s3URL.Path, "/")

	if uploadForce {
		err := API.RemoveImage(uploadAMIName, s3BucketName, s3ObjectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}

	// if no snapshot was specified, check for an existing one or a
	// snapshot task in progress
	sourceSnapshot := uploadSourceSnapshot
	if sourceSnapshot == "" {
		snapshot, err := API.FindSnapshot(uploadImageName)
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
		snapshot, err := API.CreateSnapshot(uploadImageName, s3URL.String(), uploadObjectFormat)
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
	amiID, err := API.CreateHVMImage(sourceSnapshot, uploadDiskSizeGiB, uploadAMIName, uploadAMIDescription, uploadImageArchitecture, uploadVolumeType, uploadIMDSv2Only, uploadX86BootMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create HVM image: %v\n", err)
		os.Exit(1)
	}

	if len(uploadGrantUsers) > 0 {
		err = API.GrantLaunchPermission(amiID, uploadGrantUsers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to grant launch permission: %v\n", err)
			os.Exit(1)
		}
	}

	// grant snapshot volume permission to AWS user ids
	if len(uploadGrantUsersSnapshot) > 0 {
		err = API.GrantVolumePermission(sourceSnapshot, uploadGrantUsersSnapshot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to grant snapshot volume permission: %v\n", err)
			os.Exit(1)
		}
	}

	// make the image/snaphsot public if requested
	if uploadPublic {
		err = API.PublishImage(amiID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to make image/snapshot public: %v\n", err)
			os.Exit(1)
		}
	}

	tagMap := make(map[string]string)
	for _, tag := range uploadTags {
		splitTag := strings.SplitN(tag, "=", 2)
		if len(splitTag) != 2 {
			fmt.Fprintf(os.Stderr, "invalid tag format; should be key=value, not %v\n", tag)
			os.Exit(1)
		}
		key, value := splitTag[0], splitTag[1]
		tagMap[key] = value
	}

	if err := API.CreateTags([]string{amiID, sourceSnapshot}, tagMap); err != nil {
		fmt.Fprintf(os.Stderr, "unable to add tags: %v\n", err)
		os.Exit(1)
	}

	err = json.NewEncoder(os.Stdout).Encode(&struct {
		HVM        string
		SnapshotID string
		S3Object   string
	}{
		HVM:        amiID,
		SnapshotID: sourceSnapshot,
		S3Object:   s3URL.String(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", err)
		os.Exit(1)
	}
	return nil
}
