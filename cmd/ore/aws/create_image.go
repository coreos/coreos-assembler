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

	"github.com/spf13/cobra"
)

type createSnapshotArguments struct {
	snapshotSource      string
	snapshotDescription string
}

type createImagesArguments struct {
	name        string
	description string
	createPV    bool
	snapshotID  string

	snapshotSource      string
	snapshotDescription string
}

var (
	cmdCreateSnapshot = &cobra.Command{
		Use:   "create-snapshot",
		Short: "Create AWS Snapshot",
		Long:  "Create AWS Snapshot from a vmdk url or file",
		RunE:  runCreateSnapshot,
	}
	createSnapshotArgs = &createSnapshotArguments{}

	cmdCreateImages = &cobra.Command{
		Use:   "create-images",
		Short: "Create AWS images",
		Long: `Create AWS images. This will create all relevant AMIs (hvm, pv, etc).
The flags allow controlling various knobs about the images.

After a successful run, the final line of output will be a line of JSON describing the image resources created and the underlying snapshots`,
		RunE: runCreateImages,
	}
	createImagesArgs = &createImagesArguments{}
)

func init() {
	AWS.AddCommand(cmdCreateSnapshot)
	cmdCreateSnapshot.Flags().StringVar(&createSnapshotArgs.snapshotSource, "source", "", "vmdk source: must be an 's3://' URI")
	cmdCreateSnapshot.Flags().StringVar(&createSnapshotArgs.snapshotDescription, "description", "", "Snapshot description; will be derived automatically if unset")

	AWS.AddCommand(cmdCreateImages)
	cmdCreateImages.Flags().StringVar(&createImagesArgs.name, "name", "", "[optional] the name of the image to create; will be derived automatically if unset")
	cmdCreateImages.Flags().StringVar(&createImagesArgs.description, "description", "", "[optional] the description of the image to create; will be derived automatically if unset")
	cmdCreateImages.Flags().BoolVar(&createImagesArgs.createPV, "create-pv", true, "whether to create a PV AMI in addition the the HVM AMI")
	cmdCreateImages.Flags().StringVar(&createImagesArgs.snapshotID, "snapshot-id", "", "[optional] the snapshot ID to base this AMI off of. A new snapshot will be created if not provided.")
	cmdCreateSnapshot.Flags().StringVar(&createImagesArgs.snapshotSource, "snapshot-source", "", "vmdk source: must be an 's3://' URI")
	cmdCreateSnapshot.Flags().StringVar(&createSnapshotArgs.snapshotDescription, "snapshot-description", "", "Snapshot description; will be derived automatically if unset")

}

func runCreateSnapshot(cmd *cobra.Command, args []string) error {
	snapshot, err := API.CreateSnapshot(createSnapshotArgs.snapshotDescription, createSnapshotArgs.snapshotSource)
	if err != nil {
		return fmt.Errorf("unable to create snapshot: %v", err)
	}

	fmt.Printf("created snapshot: %v\n", snapshot.SnapshotID)
	return nil
}

func runCreateImages(cmd *cobra.Command, args []string) error {
	snapshotID := createImagesArgs.snapshotID
	if snapshotID == "" {
		newSnapshotID, err := API.CreateSnapshot(createSnapshotArgs.snapshotDescription, createSnapshotArgs.snapshotSource)
		if err != nil {
			return fmt.Errorf("unable to create snapshot: %v", err)
		}
		snapshotID = newSnapshotID.SnapshotID
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
		HVM string
		PV  string `json:",omitempty"`

		SnapshotID string
	}{
		HVM:        hvmID,
		PV:         pvID,
		SnapshotID: snapshotID,
	})
	return nil
}
