// Copyright 2019 CoreOS, Inc.
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

	"github.com/coreos/mantle/platform/api/aws"
)

var (
	cmdListRegions = &cobra.Command{
		Use:   "list-regions",
		Short: "List enabled regions in the given AWS account",
		Long: `List enabled regions in the AWS account and partition implied by the
specified credentials file, profile, and region.`,
		RunE: runListRegions,

		SilenceUsage: true,
	}
	disabledRegions bool
	allRegions      bool
)

func init() {
	AWS.AddCommand(cmdListRegions)
	cmdListRegions.Flags().BoolVar(&disabledRegions, "disabled", false, "list disabled regions")
	cmdListRegions.Flags().BoolVar(&allRegions, "all", false, "list all regions")
}

func runListRegions(cmd *cobra.Command, args []string) error {
	var kind = aws.RegionEnabled
	if allRegions {
		kind = aws.RegionAny
	} else if disabledRegions {
		kind = aws.RegionDisabled
	}

	regions, err := API.ListRegions(kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not list regions: %v\n", err)
		os.Exit(1)
	}

	for _, region := range regions {
		fmt.Println(region)
	}
	return nil
}
