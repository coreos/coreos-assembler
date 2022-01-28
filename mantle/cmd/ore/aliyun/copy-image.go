// Copyright 2019 Red Hat
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

package aliyun

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdCopyImage = &cobra.Command{
		Use:   "copy-image <dest-region...>",
		Short: "Copy aliyun image between regions",
		Long: `Copy an aliyun image to one or more regions.

After a successful run, the final line of output will be a line of JSON describing the resources created.
`,
		RunE: runCopyImage,

		SilenceUsage: true,
	}

	sourceImageID        string
	destImageName        string
	destImageDescription string
	waitForReady         bool
)

func init() {
	Aliyun.AddCommand(cmdCopyImage)
	cmdCopyImage.Flags().StringVar(&sourceImageID, "image", "", "source image")
	cmdCopyImage.Flags().StringVar(&destImageName, "name", "", "destination image name")
	cmdCopyImage.Flags().StringVar(&destImageDescription, "description", "", "destination image description")
	cmdCopyImage.Flags().BoolVar(&waitForReady, "wait-for-ready", false, "wait for the copied image to be marked available")
}

func runCopyImage(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Specify one or more regions.\n")
		os.Exit(2)
	}

	ids := make(map[string]string)
	for _, region := range args {
		id, err := API.CopyImage(sourceImageID, destImageName, region, destImageDescription, "", false, waitForReady)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Copying image to region %q: %v\n", region, err)
			os.Exit(1)
		}
		ids[region] = id
	}

	err := json.NewEncoder(os.Stdout).Encode(ids)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", err)
		os.Exit(1)
	}
	return nil
}
