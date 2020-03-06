// Copyright 2019 Red Hat Inc.
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
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdListRegions = &cobra.Command{
		Use:   "list-regions",
		Short: "List enabled regions in the given aliyun account",
		Long:  `List enabled regions in the given aliyun account.`,
		RunE:  runListRegions,

		SilenceUsage: true,
	}
)

func init() {
	Aliyun.AddCommand(cmdListRegions)
}

func runListRegions(cmd *cobra.Command, args []string) error {
	regions, err := API.ListRegions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not list regions: %v\n", err)
		os.Exit(1)
	}

	for _, region := range regions {
		fmt.Println(region)
	}
	return nil
}
