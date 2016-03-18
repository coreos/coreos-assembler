// Copyright 2014 CoreOS, Inc.
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

package main

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/index"
)

var (
	indexDryRun bool
	indexForce  bool
	cmdIndex    = &cobra.Command{
		Use:   "index gs://bucket/prefix/ [gs://...]",
		Short: "Update HTML indexes",
		Long:  "Update HTML indexes for Google Storage buckets",
		Run:   runIndex,
	}
)

func init() {
	cmdIndex.Flags().BoolVarP(&indexDryRun,
		"dry-run", "n", false,
		"perform a trial run with no changes")
	cmdIndex.Flags().BoolVarP(&indexForce,
		"force", "f", false,
		"overwrite objects even if they appear up to date")
	root.AddCommand(cmdIndex)
}

func runIndex(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No URLs specified\n")
		os.Exit(2)
	}

	mode := index.WriteUpdate
	if indexDryRun {
		mode = index.WriteNever
	} else if indexForce {
		mode = index.WriteAlways
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	for _, url := range args {
		if err := index.Update(client, url, mode); err != nil {
			fmt.Fprintf(os.Stderr, "Updating indexes for %s failed: %v\n", url, err)
			os.Exit(1)
		}
	}

	if mode == index.WriteNever {
		fmt.Printf("Dry-run successful!\n")
	} else {
		fmt.Printf("Update successful!\n")
	}
}
