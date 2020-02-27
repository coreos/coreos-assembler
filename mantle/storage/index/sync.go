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

	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/storage"
)

type SyncIndexJob struct {
	storage.SyncJob
	IndexJob

	srcIndexes IndexSet
	dstIndexes IndexSet
}

func NewSyncIndexJob(src, dst *storage.Bucket) *SyncIndexJob {
	si := &SyncIndexJob{
		SyncJob: storage.SyncJob{
			Source:      src,
			Destination: dst,
		},
		IndexJob: IndexJob{
			Bucket: dst,
		},
		srcIndexes: NewIndexSet(src),
		dstIndexes: NewIndexSet(dst),
	}
	si.SyncJob.SourceFilter(si.srcIndexes.NotIndex)
	si.SyncJob.DeleteFilter(si.dstIndexes.NotIndex)
	return si
}

// DestinationPrefix overrides the Destination bucket's default prefix.
func (si *SyncIndexJob) DestinationPrefix(p string) {
	si.SyncJob.DestinationPrefix(p)
	si.IndexJob.Prefix(p)
}

// Prefix is an alias for DestinationPrefix()
func (si *SyncIndexJob) Prefix(p string) {
	si.DestinationPrefix(p)
}

// SourceFilter selects which objects to copy from Source.
func (si *SyncIndexJob) SourceFilter(f storage.Filter) {
	si.SyncJob.SourceFilter(func(obj *gs.Object) bool {
		return f(obj) && si.srcIndexes.NotIndex(obj)
	})
}

// DeleteFilter selects which objects may be pruned from Destination.
func (si *SyncIndexJob) DeleteFilter(f storage.Filter) {
	si.SyncJob.DeleteFilter(func(obj *gs.Object) bool {
		return f(obj) && si.dstIndexes.NotIndex(obj)
	})
}

// Delete enables deletion of extra objects and indexes from Destination.
func (si *SyncIndexJob) Delete(enable bool) {
	si.SyncJob.Delete(enable)
	si.IndexJob.Delete(enable)
}

// Recursive toggles copying/indexing subdirectories (the default).
func (si *SyncIndexJob) Recursive(enable bool) {
	si.SyncJob.Recursive(enable)
	si.IndexJob.Recursive(enable)
}

func (sj *SyncIndexJob) Do(ctx context.Context) error {
	if err := sj.SyncJob.Do(ctx); err != nil {
		return err
	}
	return sj.IndexJob.Do(ctx)
}
