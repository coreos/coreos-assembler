// Copyright 2018 CoreOS, Inc.
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

package do

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdDeleteKeys = &cobra.Command{
		Use:   "delete-keys <key>...",
		Short: "Delete DigitalOcean SSH keys",
		RunE:  runDeleteKeys,

		SilenceUsage: true,
	}
)

func init() {
	DO.AddCommand(cmdDeleteKeys)
}

func runDeleteKeys(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Specify at least one key.\n")
		os.Exit(2)
	}

	names := map[string]bool{}
	for _, arg := range args {
		names[arg] = true
	}

	ctx := context.Background()

	keys, err := API.ListKeys(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't list keys: %v\n", err)
		os.Exit(1)
	}

	exit := 0
	for _, key := range keys {
		if names[key.Name] {
			if err := API.DeleteKey(ctx, key.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Couldn't delete key: %v\n", key.Name)
				exit = 1
			}
			delete(names, key.Name)
		}
	}

	for name := range names {
		fmt.Fprintf(os.Stderr, "No such key: %v\n", name)
		exit = 1
	}

	os.Exit(exit)
	return nil
}
