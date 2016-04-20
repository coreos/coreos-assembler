// Copyright 2016 CoreOS, Inc.
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
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/net/context"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/storage/index"
)

var (
	releaseBatch  bool
	releaseDryRun bool
	cmdRelease    = &cobra.Command{
		Use:   "release [options]",
		Short: "Publish a new CoreOS release.",
		Run:   runRelease,
		Long: `Publish a new CoreOS release.

TODO`,
	}
)

func init() {
	cmdRelease.Flags().BoolVarP(&releaseDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	AddSpecFlags(cmdRelease.Flags())
	root.AddCommand(cmdRelease)
}

func runRelease(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}

	spec := ChannelSpec()
	ctx := context.Background()
	client, err := auth.GoogleClient()
	if err != nil {
		plog.Fatalf("Authentication failed: %v", err)
	}

	src, err := storage.NewBucket(client, spec.SourceURL())
	if err != nil {
		plog.Fatal(err)
	}
	src.WriteDryRun(releaseDryRun)

	if err := src.Fetch(ctx); err != nil {
		plog.Fatal(err)
	}

	// Sanity check!
	if vertxt := src.Object(src.Prefix() + "version.txt"); vertxt == nil {
		verurl := src.URL().String() + "version.txt"
		plog.Fatalf("File not found: %s", verurl)
	}

	// GCE

	for _, dSpec := range spec.Destinations {
		dst, err := storage.NewBucket(client, dSpec.ParentURL())
		if err != nil {
			plog.Fatal(err)
		}
		dst.WriteDryRun(releaseDryRun)

		// Fetch parent directory non-recursively to re-index it later.
		if err := dst.FetchPrefix(ctx, dst.Prefix(), false); err != nil {
			plog.Fatal(err)
		}

		// Fetch and sync each destination directory.
		for _, prefix := range dSpec.Prefixes() {
			if err := dst.FetchPrefix(ctx, prefix, true); err != nil {
				plog.Fatal(err)
			}

			sync := index.NewSyncIndexJob(src, dst)
			sync.DestinationPrefix(prefix)
			sync.DirectoryHTML(dSpec.DirectoryHTML)
			sync.IndexHTML(dSpec.IndexHTML)
			sync.Delete(true)
			if err := sync.Do(ctx); err != nil {
				plog.Fatal(err)
			}
		}

		// Now refresh the parent directory index.
		parent := index.NewIndexJob(dst)
		parent.DirectoryHTML(dSpec.DirectoryHTML)
		parent.IndexHTML(dSpec.IndexHTML)
		parent.Recursive(false)
		parent.Delete(true)
		if err := parent.Do(ctx); err != nil {
			plog.Fatal(err)
		}
	}
}
