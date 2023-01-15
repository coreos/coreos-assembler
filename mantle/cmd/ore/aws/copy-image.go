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

	"github.com/coreos/coreos-assembler/mantle/platform/api/aws"

	"github.com/spf13/cobra"
)

var (
	cmdCopyImage = &cobra.Command{
		Use:   "copy-image <dest-region...>",
		Short: "Copy AWS image between regions",
		Long: `Copy an AWS image to one or more regions.

After a successful run, the final line of output will be a line of JSON describing the resources created.
`,
		RunE: runCopyImage,

		SilenceUsage: true,
	}

	sourceImageID string
)

func init() {
	AWS.AddCommand(cmdCopyImage)
	cmdCopyImage.Flags().StringVar(&sourceImageID, "image", "", "source AMI")
}

func runCopyImage(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Specify one or more regions.\n")
		os.Exit(2)
	}

	enc := json.NewEncoder(os.Stdout)
	err := API.CopyImage(sourceImageID, args, func(region string, ami aws.ImageData) {
		enc_err := enc.Encode(map[string]aws.ImageData{region: ami})
		if enc_err != nil {
			fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", enc_err)
			os.Exit(1)
		}
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't copy images: %v\n", err)
		os.Exit(1)
	}

	return nil
}
