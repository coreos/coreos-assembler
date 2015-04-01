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

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/index"
)

var cmdIndex = &cli.Command{
	Name:        "index",
	Description: "Update HTML indexes for Google Storage buckets",
	Summary:     "Update HTML indexes",
	Usage:       "gs://bucket/prefix/ [gs://...]",
	Run:         runIndex,
}

func init() {
	cli.Register(cmdIndex)
}

func runIndex(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No URLs specified\n")
		return 2
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	for _, url := range args {
		if err := index.Update(client, url); err != nil {
			fmt.Fprintf(os.Stderr, "Updating indexes for %s failed: %v\n", url, err)
			return 1
		}
	}

	fmt.Printf("Update successful!\n")
	return 0
}
