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
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/sdk"
	"github.com/spf13/cobra"
)

type createSnapshotArguments struct {
	snapshotSource      string
	snapshotDescription string
	format              aws.EC2ImageFormat
}

type createImagesArguments struct {
	name        string
	description string
	createPV    bool
	snapshotID  string

	// All snapshot arguments are valid here too since create image can
	// implicitly create a snapshot
	*createSnapshotArguments
}

var (
	cmdCreateSnapshot = &cobra.Command{
		Use:   "create-snapshot",
		Short: "Create AWS Snapshot",
		Long: `Create AWS Snapshot from an image in S3.
Supported formats are VMDK (as created with ./image_to_vm --format=ami_vmdk) and RAW.

The image may be uploaded to S3 manually, or with the 'ore aws upload' command.

This command does not have to be used directly. The 'ore aws create-images' command will create a snapshot if necessary.`,
		RunE: runCreateSnapshot,
	}
	createSnapshotArgs = &createSnapshotArguments{}

	cmdCreateImages = &cobra.Command{
		Use:   "create-images",
		Short: "Create AWS images",
		Long: `Create AWS images. This will create all relevant AMIs (hvm, pv, etc).
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
	createImagesArgs = &createImagesArguments{
		createSnapshotArguments: &createSnapshotArguments{},
	}
)

func init() {
	addSnapshotFlags := func(cmd *cobra.Command, args *createSnapshotArguments, prefix string) {
		cmd.Flags().StringVar(&args.snapshotSource, prefix+"source", "", "snapshot source: must be an 's3://' URI; defaults to the same as upload if unset")
		cmd.Flags().StringVar(&args.snapshotDescription, prefix+"description", "", "snapshot description")
		cmd.Flags().Var(&args.format, prefix+"format", fmt.Sprintf("snapshot format: default %s, %s or %s", aws.EC2ImageFormatVmdk, aws.EC2ImageFormatVmdk, aws.EC2ImageFormatRaw))
	}

	AWS.AddCommand(cmdCreateSnapshot)
	addSnapshotFlags(cmdCreateSnapshot, createSnapshotArgs, "")

	AWS.AddCommand(cmdCreateImages)
	cmdCreateImages.Flags().StringVar(&createImagesArgs.name, "name", "", "the name of the image to create; defaults to Container-Linux-$USER-$VERSION")
	cmdCreateImages.Flags().StringVar(&createImagesArgs.description, "description", "", "the description of the image to create")
	cmdCreateImages.Flags().BoolVar(&createImagesArgs.createPV, "create-pv", true, "whether to create a PV AMI in addition the the HVM AMI")
	cmdCreateImages.Flags().StringVar(&createImagesArgs.snapshotID, "snapshot-id", "", "[optional] the snapshot ID to base this AMI off of. A new snapshot will be created if not provided.")
	addSnapshotFlags(cmdCreateImages, createImagesArgs.createSnapshotArguments, "snapshot-")
}

func createSnapshot(args *createSnapshotArguments) (string, error) {
	snapshotSource, err := defaultBucketURI(args.snapshotSource, "", "", "", region)

	if err != nil {
		return "", fmt.Errorf("unable to guess snapshot source: %v", err)
	}
	snapshot, err := API.CreateSnapshot(args.snapshotDescription, snapshotSource, args.format)
	if err != nil {
		return "", fmt.Errorf("unable to create snapshot: %v", err)
	}

	return snapshot.SnapshotID, nil
}

func runCreateSnapshot(cmd *cobra.Command, args []string) error {
	snapshotID, err := createSnapshot(createSnapshotArgs)
	if err != nil {
		return err
	}
	json.NewEncoder(os.Stdout).Encode(&struct {
		SnapshotID string
	}{
		SnapshotID: snapshotID,
	})
	return nil
}

func runCreateImages(cmd *cobra.Command, args []string) error {
	if createImagesArgs.name == "" {
		buildDir := sdk.BuildRoot() + "/images/amd64-usr/latest/coreos_production_ami_vmdk_image.vmdk"
		ver, err := sdk.VersionsFromDir(filepath.Dir(buildDir))
		if err != nil {
			return fmt.Errorf("could not guess image name: %v", err)
		}
		awsVersion := strings.Replace(ver.Version, "+", "-", -1) // '+' is invalid in an AMI name
		createImagesArgs.name = fmt.Sprintf("Container-Linux-dev-%s-%s", os.Getenv("USER"), awsVersion)
	}

	snapshotID := createImagesArgs.snapshotID
	if snapshotID == "" {
		newSnapshotID, err := createSnapshot(createImagesArgs.createSnapshotArguments)
		if err != nil {
			return fmt.Errorf("unable to create snapshot: %v", err)
		}
		snapshotID = newSnapshotID
	}

	hvmID, err := API.CreateHVMImage(snapshotID, createImagesArgs.name, createImagesArgs.description)
	if err != nil {
		return fmt.Errorf("unable to create HVM image: %v", err)
	}
	var pvID string
	if createImagesArgs.createPV {
		pvImageID, err := API.CreatePVImage(snapshotID, createImagesArgs.name, createImagesArgs.description)
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
