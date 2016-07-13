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
	"os"
	"path/filepath"

	"github.com/coreos/go-semver/semver"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/sdk/repo"
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
	sdkVersion     string
	manifestURL    string
	manifestName   string
	manifestBranch string
	repoVerify     bool

	// only for `create` command
	allowReplace bool

	// only for `enter` command
	experimental bool

	// only for `update` command
	allowCreate      bool
	downgradeInPlace bool
	downgradeReplace bool
	newVersion       string

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
	updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update the SDK chroot and source tree",
		Run:   runUpdate,
	}
	verifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check repo tree and release manifest match",
		Run:   runVerify,
	}
)

func init() {
	// the names and error handling of these flag sets are meaningless,
	// the flag sets are only used to group common options together.
	chrootFlags = pflag.NewFlagSet("chroot", pflag.ExitOnError)
	chrootFlags.StringVar(&chrootName,
		"chroot", "chroot", "SDK chroot directory name")

	creationFlags = pflag.NewFlagSet("creation", pflag.ExitOnError)
	creationFlags.StringVar(&sdkVersion,
		"sdk-version", "", "SDK version. Defaults to the SDK version in version.txt")
	creationFlags.StringVar(&manifestURL,
		"manifest-url", coreosManifestURL, "Manifest git repo location")
	creationFlags.StringVar(&manifestBranch,
		"manifest-branch", "master", "Manifest git repo branch")
	creationFlags.StringVar(&manifestName,
		"manifest-name", "default.xml", "Manifest file name")
	creationFlags.BoolVar(&repoVerify,
		"verify", false, "Check repo tree and release manifest match")

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

	updateCmd.Flags().AddFlagSet(chrootFlags)
	updateCmd.Flags().AddFlagSet(creationFlags)
	updateCmd.Flags().BoolVar(&allowCreate,
		"create", false, "Create the SDK chroot if missing")
	updateCmd.Flags().BoolVar(&downgradeInPlace,
		"downgrade-in-place", false,
		"Allow in-place downgrades of SDK chroot")
	updateCmd.Flags().BoolVar(&downgradeReplace,
		"downgrade-replace", false,
		"Replace SDK chroot instead of downgrading")
	updateCmd.Flags().StringVar(&newVersion,
		"new-version", "", "Hint at the new version. Defaults to the version in version.txt")
	root.AddCommand(updateCmd)

	root.AddCommand(verifyCmd)
}

func runCreate(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if sdkVersion == "" {
		plog.Noticef("Detecting SDK version")

		if ver, err := sdk.VersionsFromManifest(); err == nil {
			sdkVersion = ver.SDKVersion
			plog.Noticef("Found SDK version %s from local repo", sdkVersion)
		} else if ver, err := sdk.VersionsFromRemoteRepo(manifestURL, manifestBranch); err == nil {
			sdkVersion = ver.SDKVersion
			plog.Noticef("Found SDK version %s from remote repo", sdkVersion)
		} else {
			plog.Fatalf("Reading from remote repo failed: %v", err)
		}
	}

	unpackChroot(allowReplace)
	updateRepo()
}

func unpackChroot(replace bool) {
	plog.Noticef("Downloading SDK version %s", sdkVersion)
	if err := sdk.DownloadSDK(sdkVersion); err != nil {
		plog.Fatalf("Download failed: %v", err)
	}

	if replace {
		if err := sdk.Delete(chrootName); err != nil {
			plog.Fatalf("Replace failed: %v", err)
		}
	}

	if err := sdk.Unpack(sdkVersion, chrootName); err != nil {
		plog.Fatalf("Create failed: %v", err)
	}

	if err := sdk.Setup(chrootName); err != nil {
		plog.Fatalf("Create failed: %v", err)
	}
}

func updateRepo() {
	if err := sdk.RepoInit(chrootName, manifestURL, manifestBranch, manifestName); err != nil {
		plog.Fatalf("repo init failed: %v", err)
	}

	if err := sdk.RepoSync(chrootName); err != nil {
		plog.Fatalf("repo sync failed: %v", err)
	}

	if repoVerify {
		if err := repo.VerifySync(manifestName); err != nil {
			plog.Fatalf("Verify failed: %v", err)
		}
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

func verLessThan(a, b string) bool {
	aver, err := semver.NewVersion(a)
	if err != nil {
		plog.Fatal(err)
	}
	bver, err := semver.NewVersion(b)
	if err != nil {
		plog.Fatal(err)
	}
	return aver.LessThan(*bver)
}

func runUpdate(cmd *cobra.Command, args []string) {
	const updateChroot = "/mnt/host/source/src/scripts/update_chroot"
	updateCommand := append([]string{updateChroot}, args...)

	// avoid downgrade strategy ambiguity
	if downgradeInPlace && downgradeReplace {
		plog.Fatal("Conflicting downgrade options")
	}

	if sdkVersion == "" || newVersion == "" {
		plog.Notice("Detecting versions in remote repo")
		ver, err := sdk.VersionsFromRemoteRepo(manifestURL, manifestBranch)
		if err != nil {
			plog.Fatalf("Reading from remote repo failed: %v", err)
		}

		if newVersion == "" {
			newVersion = ver.Version
		}

		if sdkVersion == "" {
			sdkVersion = ver.SDKVersion
		}
	}

	plog.Infof("New version %s", newVersion)
	plog.Infof("SDK version %s", sdkVersion)

	plog.Info("Checking version of local chroot")
	chroot := filepath.Join(sdk.RepoRoot(), chrootName)
	old, err := sdk.OSRelease(chroot)
	if err != nil {
		if allowCreate && os.IsNotExist(err) {
			unpackChroot(false)
		} else {
			plog.Fatal(err)
		}
	} else if verLessThan(newVersion, old.Version) {
		plog.Noticef("Downgrade from %s to %s required!",
			old.Version, newVersion)
		if downgradeReplace {
			unpackChroot(true)
		} else if downgradeInPlace {
			plog.Infof("Attempting to downgrade existing chroot.")
		} else {
			plog.Fatalf("Refusing to downgrade.")
		}
	}

	updateRepo()

	if err := sdk.Enter(chrootName, updateCommand...); err != nil {
		plog.Fatalf("update_chroot failed: %v", err)
	}
}

func runVerify(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if err := repo.VerifySync(""); err != nil {
		plog.Fatalf("Verify failed: %v", err)
	}
}
