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

package gcloud

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/storage/index"
)

var (
	indexDryRun    bool
	indexForce     bool
	indexDelete    bool
	indexDirs      bool
	indexRecursive bool
	indexTitle     string
	cmdIndex       = &cobra.Command{
		Use:   "index [options] gs://bucket/prefix/ [gs://...]",
		Short: "Update HTML indexes",
		Run:   runIndex,
		Long: `Update HTML indexes for Google Storage.

Scan a given Google Storage location and generate "index.html" under
every directory prefix. If the --directories option is given then
objects matching the directory prefixes are also created. For example,
the pages generated for a bucket containing only "dir/obj":

    index.html     - a HTML index page listing dir
    dir/index.html - a HTML index page listing obj
    dir/           - an identical HTML index page
    dir            - a redirect page to dir/

Do not enable --directories if you expect to be able to copy the tree to
a local filesystem, the fake directories will conflict with the real ones!`,
	}
)

func init() {
	cmdIndex.Flags().BoolVarP(&indexDryRun,
		"dry-run", "n", false,
		"perform a trial run with no changes")
	cmdIndex.Flags().BoolVarP(&indexForce,
		"force", "f", false,
		"overwrite objects even if they appear up to date")
	cmdIndex.Flags().BoolVar(&indexDelete,
		"delete", false, "delete index objects")
	cmdIndex.Flags().BoolVarP(&indexRecursive, "recursive", "r", false,
		"update nested prefixes")
	cmdIndex.Flags().BoolVarP(&indexDirs,
		"directories", "D", false,
		"use objects to mimic a directory tree")
	cmdIndex.Flags().StringVarP(&indexTitle, "html-title", "T", "",
		"use the given title instead of bucket name in index pages")
	GCloud.AddCommand(cmdIndex)
}

func runIndex(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No URLs specified\n")
		os.Exit(2)
	}

	ctx := context.Background()
	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	for _, url := range args {
		if err := updateTree(ctx, client, url); err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
			os.Exit(1)
		}
	}

	if indexDryRun {
		fmt.Printf("Dry-run successful!\n")
	} else {
		fmt.Printf("Update successful!\n")
	}
}

func updateTree(ctx context.Context, client *http.Client, url string) error {
	root, err := storage.NewBucket(client, url)
	if err != nil {
		return err
	}
	root.WriteDryRun(indexDryRun)
	root.WriteAlways(indexForce)

	if err = root.FetchPrefix(ctx, root.Prefix(), indexRecursive); err != nil {
		return err
	}

	job := index.IndexJob{Bucket: root}
	job.DirectoryHTML(indexDirs)
	job.IndexHTML(true)
	job.Delete(indexDelete)
	job.Recursive(indexRecursive)
	if indexTitle != "" {
		job.Name(indexTitle)
	}
	return job.Do(ctx)
}
