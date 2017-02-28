// Copyright 2015 CoreOS, Inc.
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
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/sdk/omaha"
)

var (
	buildCmd = &cobra.Command{
		Use:   "build [object]",
		Short: "Build something",
	}
	buildUpdateCmd = &cobra.Command{
		Use:   "update",
		Short: "Build an image update payload",
		Run:   runBuildUpdate,
	}
)

func init() {
	buildCmd.AddCommand(buildUpdateCmd)
	root.AddCommand(buildCmd)
}

func runBuildUpdate(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatalf("Unrecognized arguments: %v", args)
	}

	err := omaha.GenerateFullUpdate(sdk.BuildImageDir("", ""))
	if err != nil {
		plog.Fatalf("Building full update failed: %v", err)
	}
}
