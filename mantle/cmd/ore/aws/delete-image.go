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
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdDeleteImage = &cobra.Command{
		Use:   "delete-image --ami <ami_id> --snapshot <snapshot_id> ...",
		Short: "Delete AMI and/or snapshot",
		Run:   runDeleteImage,
	}
	amiID        string
	snapshotID   string
	allowMissing bool
)

func init() {
	// Initialize the command and its flags
	AWS.AddCommand(cmdDeleteImage)
	cmdDeleteImage.Flags().StringVar(&amiID, "ami", "", "AWS ami tag")
	cmdDeleteImage.Flags().StringVar(&snapshotID, "snapshot", "", "AWS snapshot tag")
	cmdDeleteImage.Flags().BoolVar(&allowMissing, "allow-missing", false, "Do not error out on the resource not existing")
}

func runDeleteImage(cmd *cobra.Command, args []string) {
	// Check if either amiID or snapshotID is provided
	if amiID == "" && snapshotID == "" {
		fmt.Fprintf(os.Stderr, "Provide --ami or --snapshot to delete\n")
		os.Exit(1)
	}

	// Remove resources based on provided flags
	if amiID != "" {
		err := API.RemoveByAmiTag(amiID, allowMissing)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not delete %v: %v\n", amiID, err)
			os.Exit(1)
		}
	}

	if snapshotID != "" {
		if snapshotID == "detectFromAMI" && amiID != "" {
			// Let's try to extract the snapshotID from AMI
			snapshot, err := API.FindSnapshot(amiID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Encountered error when searching for snapshot for %s: %s. Continuing..\n", amiID, err)
				os.Exit(0)
			} else if snapshot == nil {
				fmt.Fprintf(os.Stdout, "No valid snapshot found for %s.\n", amiID)
				os.Exit(0)
			}
			snapshotID = snapshot.SnapshotID
		}
		err := API.RemoveBySnapshotTag(snapshotID, allowMissing)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not delete %v: %v\n", snapshotID, err)
			os.Exit(1)
		}
	}

	os.Exit(0)
}
