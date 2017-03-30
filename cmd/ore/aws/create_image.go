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

var (
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
	AWS.AddCommand(cmdCreateImages)
	cmdCreateImages.Flags().StringVar(&name, "name", "", "the name of the image to create; defaults to Container-Linux-$USER-$VERSION")
	cmdCreateImages.Flags().StringVar(&description, "description", "", "the description of the image to create")
	cmdCreateImages.Flags().BoolVar(&createPV, "create-pv", true, "whether to create a PV AMI in addition the the HVM AMI")
	cmdCreateImages.Flags().StringVar(&snapshotID, "snapshot-id", "", "[optional] the snapshot ID to base this AMI off of. A new snapshot will be created if not provided.")
	cmdCreateImages.Flags().StringVar(&snapshotSource, "snapshot-source", "", "snapshot source: must be an 's3://' URI; defaults to the same as upload if unset")
	cmdCreateImages.Flags().StringVar(&snapshotDescription, "snapshot-description", "", "snapshot description")
	cmdCreateImages.Flags().Var(&format, "snapshot-format", fmt.Sprintf("snapshot format: default %s, %s or %s", aws.EC2ImageFormatVmdk, aws.EC2ImageFormatVmdk, aws.EC2ImageFormatRaw))
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
