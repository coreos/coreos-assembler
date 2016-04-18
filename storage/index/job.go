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

package index

import (
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/net/context"

	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/storage"
)

type IndexJob struct {
	Bucket *storage.Bucket

	enableDirectoryHTML bool
	enableIndexHTML     bool
	enableDelete        bool
}

// DirectoryHTML enables generation of HTML pages to mimic directories.
func (ij *IndexJob) DirectoryHTML(enable bool) {
	ij.enableDirectoryHTML = enable
}

// IndexHTML enables generation of index.html pages for each directory.
func (ij *IndexJob) IndexHTML(enable bool) {
	ij.enableIndexHTML = enable
}

// Delete enables deletion of stale indexes for now empty directories.
func (ij *IndexJob) Delete(enable bool) {
	ij.enableDelete = enable
}

func (ij *IndexJob) Do(ctx context.Context) error {
	wg := worker.NewWorkerGroup(ctx, storage.MaxConcurrentRequests)
	tree := NewIndexTree(ij.Bucket)
	var doDir func(string) error
	doDir = func(dir string) error {
		ix := tree.Indexer(dir)
		if ij.enableDirectoryHTML {
			if err := wg.Start(ix.UpdateRedirect); err != nil {
				return err
			}
			if err := wg.Start(ix.UpdateDirectoryHTML); err != nil {
				return err
			}
		} else if ij.enableDelete {
			if err := wg.Start(ix.DeleteRedirect); err != nil {
				return err
			}
			if err := wg.Start(ix.DeleteDirectory); err != nil {
				return err
			}
		}
		if ij.enableIndexHTML {
			if err := wg.Start(ix.UpdateIndexHTML); err != nil {
				return err
			}
		} else if ij.enableDelete {
			if err := wg.Start(ix.DeleteIndexHTML); err != nil {
				return err
			}
		}
		for _, subdir := range ix.SubDirs {
			if err := doDir(subdir); err != nil {
				return err
			}
		}
		return nil
	}

	if err := doDir(ij.Bucket.Prefix()); err != nil {
		return wg.WaitError(err)
	}

	if ij.enableDelete {
		for _, index := range tree.EmptyIndexes(ij.Bucket.Prefix()) {
			objName := index
			if err := wg.Start(func(ctx context.Context) error {
				return ij.Bucket.Delete(ctx, objName)
			}); err != nil {
				return wg.WaitError(err)
			}
		}
	}

	return wg.Wait()
}
