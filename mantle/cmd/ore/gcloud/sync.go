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

package gcloud

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/storage/index"
)

var (
	syncDryRun     bool
	syncForce      bool
	syncDelete     bool
	syncRecursive  bool
	syncIndexDirs  bool
	syncIndexPages bool
	syncIndexTitle string
	cmdSync        = &cobra.Command{
		Use:   "sync gs://src/foo gs://dst/bar",
		Short: "Copy objects between GS buckets",
		Run:   runSync,
	}
)

func init() {
	cmdSync.Flags().BoolVarP(&syncDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	cmdSync.Flags().BoolVarP(&syncForce, "force", "f", false,
		"write everything, even when already up-to-date")
	cmdSync.Flags().BoolVar(&syncDelete, "delete", false,
		"delete extra objects and indexes")
	cmdSync.Flags().BoolVarP(&syncRecursive, "recursive", "r", false,
		"sync nested prefixes")
	cmdSync.Flags().BoolVarP(&syncIndexDirs, "index-dirs", "D", false,
		"generate HTML pages to mimic a directory tree")
	cmdSync.Flags().BoolVarP(&syncIndexPages, "index-html", "I", false,
		"generate index.html pages for each directory")
	cmdSync.Flags().StringVarP(&syncIndexTitle, "html-title", "T", "",
		"use the given title instead of bucket name in index pages")
	GCloud.AddCommand(cmdSync)
}

func runSync(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "Expected exactly two gs:// URLs. Got: %v\n", args)
		os.Exit(2)
	}

	ctx := context.Background()
	src, err := storage.NewBucket(api.Client(), args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	src.WriteDryRun(true) // do not write to src

	dst, err := storage.NewBucket(api.Client(), args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	dst.WriteDryRun(syncDryRun)
	dst.WriteAlways(syncForce)

	err = worker.Parallel(ctx,
		func(c context.Context) error {
			return src.FetchPrefix(c, src.Prefix(), syncRecursive)
		},
		func(c context.Context) error {
			return dst.FetchPrefix(c, dst.Prefix(), syncRecursive)
		})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	job := index.NewSyncIndexJob(src, dst)
	job.DirectoryHTML(syncIndexDirs)
	job.IndexHTML(syncIndexPages)
	job.Delete(syncDelete)
	job.Recursive(syncRecursive)
	if syncIndexTitle != "" {
		job.Name(syncIndexTitle)
	}
	if err := job.Do(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
