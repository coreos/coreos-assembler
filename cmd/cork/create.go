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
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/sdk"
)

var (
	chrootVersion string
	chrootName    string
	allowReplace  bool
	createCmd     = &cobra.Command{
		Use:   "create",
		Short: "Download and unpack the SDK",
		Run:   runCreate,
	}
	deleteCmd = &cobra.Command{
		Use:   "delete",
		Short: "Delete the SDK chroot",
		Run:   runDelete,
	}
)

func init() {
	createCmd.Flags().StringVar(&chrootVersion,
		"sdk-version", "", "SDK version")
	createCmd.Flags().StringVar(&chrootName,
		"chroot", "chroot", "SDK chroot directory name")
	createCmd.Flags().BoolVar(&allowReplace,
		"replace", false, "Replace an existing SDK chroot")
	deleteCmd.Flags().StringVar(&chrootName,
		"chroot", "chroot", "SDK chroot directory name")
	root.AddCommand(createCmd)
	root.AddCommand(deleteCmd)
}

func runCreate(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if chrootVersion == "" {
		plog.Fatal("Missing --sdk-version=VERSION")
	}

	plog.Noticef("Downloading SDK version %s", chrootVersion)
	if err := sdk.DownloadSDK(chrootVersion); err != nil {
		plog.Fatalf("Download failed: %v", err)
	}

	if allowReplace {
		if err := sdk.Delete(chrootName); err != nil {
			plog.Fatalf("Replace failed: %v", err)
		}
	}

	if err := sdk.Unpack(chrootVersion, chrootName); err != nil {
		plog.Fatalf("Create failed: %v", err)
	}

	if err := sdk.Setup(chrootName); err != nil {
		plog.Fatalf("Create failed: %v", err)
	}
}

func runDelete(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if err := sdk.Delete(chrootName); err != nil {
		plog.Fatalf("Delete failed: %v", err)
	}
}
