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
)

var (
	downloadCmd = &cobra.Command{
		Use:   "download",
		Short: "Download the SDK tarball",
		Long:  "Download the current SDK tarball to a local cache.",
		Run:   runDownload,
	}
	downloadVersion string
)

func init() {
	downloadCmd.Flags().StringVar(&downloadVersion,
		"sdk-version", "", "SDK version")
	root.AddCommand(downloadCmd)
}

func runDownload(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if downloadVersion == "" {
		plog.Fatal("Missing --sdk-version=VERSION")
	}

	plog.Noticef("Downloading SDK version %s", downloadVersion)
	if err := sdk.DownloadSDK(downloadVersion); err != nil {
		plog.Fatalf("Download failed: %v", err)
	}
}
