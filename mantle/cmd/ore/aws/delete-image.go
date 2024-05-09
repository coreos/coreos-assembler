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
		Use:   "delete-image <name>...",
		Short: "Delete AMIs/Snapshots",
		Run:   runDeleteImage,
	}
	resourceID   string
)

func init() {
	AWS.AddCommand(cmdDeleteImage)
	cmdDeleteImage.Flags().StringVar(&resourceID, "image", "", "AWS tag")
}

func runDeleteImage(cmd *cobra.Command, args []string) {
	exit := 0
	err := API.RemoveByImageID(resourceID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not delete resource: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exit)
}
