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
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/pflag"
	"github.com/coreos/mantle/sdk"
)

const (
	coreosManifestURL = "https://github.com/coreos/manifest.git"
)

var (
	// everything uses this flag
	chrootFlags *pflag.FlagSet
	chrootName  string

	// creation flags
	creationFlags  *pflag.FlagSet
	chrootVersion  string
	manifestURL    string
	manifestName   string
	manifestBranch string

	// only for `create` command
	allowReplace bool

	// only for `enter` command
	experimental bool

	createCmd = &cobra.Command{
		Use:   "create",
		Short: "Download and unpack the SDK",
		Run:   runCreate,
	}
	enterCmd = &cobra.Command{
		Use:   "enter [-- command]",
		Short: "Enter the SDK chroot, optionally running a command",
		Run:   runEnter,
	}
	deleteCmd = &cobra.Command{
		Use:   "delete",
		Short: "Delete the SDK chroot",
		Run:   runDelete,
	}
)

func init() {
	// the names and error handling of these flag sets are meaningless,
	// the flag sets are only used to group common options together.
	chrootFlags = pflag.NewFlagSet("chroot", pflag.ExitOnError)
	chrootFlags.StringVar(&chrootName,
		"chroot", "chroot", "SDK chroot directory name")

	creationFlags = pflag.NewFlagSet("creation", pflag.ExitOnError)
	creationFlags.StringVar(&chrootVersion,
		"sdk-version", "", "SDK version. Defaults to the SDK version in version.txt")
	creationFlags.StringVar(&manifestURL,
		"manifest-url", coreosManifestURL, "Manifest git repo location")
	creationFlags.StringVar(&manifestBranch,
		"manifest-branch", "master", "Manifest git repo branch")
	creationFlags.StringVar(&manifestName,
		"manifest-name", "default.xml", "Manifest file name")

	createCmd.Flags().AddFlagSet(chrootFlags)
	createCmd.Flags().AddFlagSet(creationFlags)
	createCmd.Flags().BoolVar(&allowReplace,
		"replace", false, "Replace an existing SDK chroot")
	root.AddCommand(createCmd)

	enterCmd.Flags().AddFlagSet(chrootFlags)
	enterCmd.Flags().BoolVar(&experimental,
		"experimental", false, "Use new enter implementation")
	root.AddCommand(enterCmd)

	deleteCmd.Flags().AddFlagSet(chrootFlags)
	root.AddCommand(deleteCmd)
}

func runCreate(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if chrootVersion == "" {
		plog.Noticef("Detecting SDK version")

		if ver, err := sdk.VersionsFromManifest(); err == nil {
			chrootVersion = ver.SDKVersion
			plog.Noticef("Found SDK version %s from local repo", chrootVersion)
		} else if ver, err := sdk.VersionsFromRemoteRepo(manifestURL, manifestBranch); err == nil {
			chrootVersion = ver.SDKVersion
			plog.Noticef("Found SDK version %s from remote repo", chrootVersion)
		} else {
			plog.Fatalf("Reading from remote repo failed: %v", err)
		}
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

	if err := sdk.RepoInit(chrootName, manifestURL, manifestBranch, manifestName); err != nil {
		plog.Fatalf("repo init failed: %v", err)
	}

	if err := sdk.RepoSync(chrootName); err != nil {
		plog.Fatalf("repo sync failed: %v", err)
	}
}

func runEnter(cmd *cobra.Command, args []string) {
	enter := sdk.OldEnter
	if experimental {
		enter = sdk.Enter
	}

	if err := enter(chrootName, args...); err != nil && len(args) != 0 {
		plog.Fatalf("Running %v failed: %v", args, err)
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
