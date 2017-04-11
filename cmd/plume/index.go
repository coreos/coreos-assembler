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
	"path"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/storage/index"
)

var (
	indexDryRun bool
	cmdIndex    = &cobra.Command{
		Use:   "index [options]",
		Short: "Update HTML indexes for download sites.",
		Run:   runIndex,
		Long: `Update some or all HTML indexes for download sites.

By default only a single release is updated but specifying the
special board, channel, and/or version 'all' will work too.

To update everything all at once:

    plume index --channel=all --board=all --version=all
    
If more flexibility is required use ore index instead.`,
	}
)

func init() {
	cmdIndex.Flags().BoolVarP(&indexDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	AddSpecFlags(cmdIndex.Flags())
	root.AddCommand(cmdIndex)
}

func runIndex(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}

	if specChannel == "all" {
		specChannel = ""
	}
	if specBoard == "all" {
		specBoard = ""
	}
	if specVersion == "all" {
		specVersion = ""
	}

	if specChannel != "" {
		if _, ok := specs[specChannel]; !ok && specChannel != "" {
			plog.Fatalf("Unknown channel: %s", specChannel)
		}
	}

	if specBoard != "" {
		boardOk := false
	channelLoop:
		for channel, spec := range specs {
			if specChannel != "" && specChannel != channel {
				continue
			}
			for _, board := range spec.Boards {
				if specBoard == board {
					boardOk = true
					break channelLoop
				}
			}
		}
		if !boardOk {
			plog.Fatalf("Unknown board: %s", specBoard)
		}
	}

	ctx := context.Background()
	client, err := auth.GoogleClient()
	if err != nil {
		plog.Fatalf("Authentication failed: %v", err)
	}

	for channel, spec := range specs {
		if specChannel != "" && specChannel != channel {
			continue
		}

		for _, dSpec := range spec.Destinations {
			if specVersion != "" &&
				!dSpec.VersionPath &&
				specVersion != dSpec.NamedPath {
				continue
			}

			bkt, err := storage.NewBucket(client, dSpec.BaseURL)
			if err != nil {
				plog.Fatal(err)
			}
			bkt.WriteDryRun(indexDryRun)

			doIndex := func(prefix string, recursive bool) {
				if err := bkt.FetchPrefix(ctx, prefix, recursive); err != nil {
					plog.Fatal(err)
				}

				job := index.NewIndexJob(bkt)
				job.DirectoryHTML(dSpec.DirectoryHTML)
				job.IndexHTML(dSpec.IndexHTML)
				job.Recursive(recursive)
				job.Prefix(prefix)
				job.Delete(true)
				if dSpec.Title != "" {
					job.Name(dSpec.Title)
				}
				if err := job.Do(ctx); err != nil {
					plog.Fatal(err)
				}
			}

			if specBoard == "" && specVersion == "" {
				doIndex(bkt.Prefix(), true)
				continue
			}

			doIndex(bkt.Prefix(), false)
			for _, board := range spec.Boards {
				if specBoard != "" && specBoard != board {
					continue
				}

				prefix := path.Join(bkt.Prefix(), board)
				if specVersion == "" {
					doIndex(prefix, true)
					continue
				}

				doIndex(prefix, false)
				doIndex(path.Join(prefix, specVersion), true)
			}
		}
	}
}
