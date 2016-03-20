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
	"net/http"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/index"
)

// Arbitrary limit on the number of concurrent jobs
const maxWriters = 12

var (
	indexDryRun bool
	indexForce  bool
	indexDirs   bool
	cmdIndex    = &cobra.Command{
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
	cmdIndex.Flags().BoolVarP(&indexDirs,
		"directories", "D", false,
		"generate objects to mimic a directory tree")
	root.AddCommand(cmdIndex)
}

func runIndex(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No URLs specified\n")
		os.Exit(2)
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	for _, url := range args {
		if err := updateTree(client, url); err != nil {
			fmt.Fprintf(os.Stderr, "Updating indexes for %s failed: %v\n", url, err)
			os.Exit(1)
		}
	}

	if indexDryRun {
		fmt.Printf("Dry-run successful!\n")
	} else {
		fmt.Printf("Update successful!\n")
	}
}

func updateTree(client *http.Client, url string) error {
	root, err := index.NewDirectory(url)
	if err != nil {
		return err
	}

	if err = root.Fetch(client); err != nil {
		return err
	}

	mode := index.WriteUpdate
	if indexDryRun {
		mode = index.WriteNever
	} else if indexForce {
		mode = index.WriteAlways
	}

	indexers := []index.Indexer{index.NewHtmlIndexer(client, mode)}
	if indexDirs {
		indexers = append(indexers,
			index.NewDirIndexer(client, mode),
			index.NewRedirector(client, mode))
	}

	dirs := make(chan *index.Directory)
	done := make(chan struct{})
	errc := make(chan error)

	// Feed the directory tree into the writers.
	go func() {
		root.Walk(dirs)
		close(dirs)
	}()

	writer := func() {
		for {
			select {
			case d, ok := <-dirs:
				if !ok {
					errc <- nil
					return
				}
				for _, ix := range indexers {
					if err := ix.Index(d); err != nil {
						errc <- err
						return
					}
				}
			case <-done:
				errc <- nil
				return
			}
		}
	}

	for i := 0; i < maxWriters; i++ {
		go writer()
	}

	// Wait for writers to finish, aborting and returning the first error.
	var ret error
	for i := 0; i < maxWriters; i++ {
		err := <-errc
		if err == nil {
			continue
		}
		if done != nil {
			close(done)
			done = nil
		}
		if ret == nil {
			ret = err
		}
	}

	return ret
}
