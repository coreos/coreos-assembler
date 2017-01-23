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

	"github.com/spf13/cobra"
)

var (
	cmdCreateSnapshot = &cobra.Command{
		Use:   "create-snapshot",
		Short: "Create AWS Snapshot",
		Long:  "Create AWS Snapshot from a vmdk url or file",
		RunE:  runCreateSnapshot,
	}

	snapshotSource      string
	snapshotDescription string
)

func init() {
	cmdCreateSnapshot.Flags().StringVar(&snapshotSource, "source", "", "vmdk source: must be an 's3://' URI")
	cmdCreateSnapshot.Flags().StringVar(&snapshotDescription, "description", "", "Snapshot description; will be derived automatically if unset")

	AWS.AddCommand(cmdCreateSnapshot)
}

func runCreateSnapshot(cmd *cobra.Command, args []string) error {
	snapshot, err := API.CreateSnapshot(snapshotDescription, snapshotSource)
	if err != nil {
		return fmt.Errorf("unable to create snapshot: %v", err)
	}

	fmt.Printf("created snapshot: %v\n", snapshot.SnapshotID)
	return nil
}
