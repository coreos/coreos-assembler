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
	"golang.org/x/net/context"

	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/storage"
)

type IndexJob struct {
	Bucket *storage.Bucket

	name                *string
	prefix              *string
	enableDirectoryHTML bool
	enableIndexHTML     bool
	enableDelete        bool
	notRecursive        bool // inverted because recursive is default
}

func NewIndexJob(bucket *storage.Bucket) *IndexJob {
	return &IndexJob{Bucket: bucket}
}

// Name overrides Bucket's name in page titles.
func (ij *IndexJob) Name(name string) {
	ij.name = &name
}

// Prefix overrides Bucket's default prefix.
func (ij *IndexJob) Prefix(p string) {
	p = storage.FixPrefix(p)
	ij.prefix = &p
}

// DirectoryHTML toggles generation of HTML pages to mimic directories.
func (ij *IndexJob) DirectoryHTML(enable bool) {
	ij.enableDirectoryHTML = enable
}

// IndexHTML toggles generation of index.html pages for each directory.
func (ij *IndexJob) IndexHTML(enable bool) {
	ij.enableIndexHTML = enable
}

// Delete toggles deletion of stale indexes for now empty directories.
func (ij *IndexJob) Delete(enable bool) {
	ij.enableDelete = enable
}

// Recursive toggles generation of indexes for subdirectories (the default).
func (sj *IndexJob) Recursive(enable bool) {
	sj.notRecursive = !enable
}

func (ij *IndexJob) doDir(wg *worker.WorkerGroup, ix *Indexer) error {
	if ij.enableDirectoryHTML && !ix.Empty() {
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

	if ij.enableIndexHTML && !ix.Empty() {
		if err := wg.Start(ix.UpdateIndexHTML); err != nil {
			return err
		}
	} else if ij.enableDelete {
		if err := wg.Start(ix.DeleteIndexHTML); err != nil {
			return err
		}
	}

	return nil
}

func (ij *IndexJob) Do(ctx context.Context) error {
	if ij.name == nil {
		name := ij.Bucket.Name()
		ij.name = &name
	}
	if ij.prefix == nil {
		prefix := ij.Bucket.Prefix()
		ij.prefix = &prefix
	}

	tree := NewIndexTree(ij.Bucket, ij.notRecursive)
	wg := worker.NewWorkerGroup(ctx, storage.MaxConcurrentRequests)

	if ij.notRecursive {
		ix := tree.Indexer(*ij.name, *ij.prefix)
		return wg.WaitError(ij.doDir(wg, ix))
	}

	for _, prefix := range tree.Prefixes(*ij.prefix) {
		ix := tree.Indexer(*ij.name, prefix)
		if err := ij.doDir(wg, ix); err != nil {
			return wg.WaitError(err)
		}
	}

	return wg.Wait()
}
