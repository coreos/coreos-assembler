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
	"flag"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/sdk"
)

var (
	cmdDownload = &cli.Command{
		Name:        "download",
		Summary:     "Download the SDK tarball",
		Description: "Download the current SDK tarball to a local cache.",
		Flags:       *flag.NewFlagSet("download", flag.ExitOnError),
		Run:         runDownload,
	}
	downloadVersion string
)

func init() {
	cmdDownload.Flags.StringVar(&downloadVersion,
		"sdk-version", "", "SDK version")

	cli.Register(cmdDownload)
}

func runDownload(args []string) int {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if downloadVersion == "" {
		plog.Fatal("Missing --sdk-version=VERSION")
	}

	plog.Noticef("Downloading SDK version %s", downloadVersion)
	if err := sdk.Download(downloadVersion); err != nil {
		plog.Fatalf("Download failed: %v", err)
	}

	return 0
}
