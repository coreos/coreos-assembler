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

	gs "github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/storage"
)

type SyncIndexJob struct {
	storage.SyncJob
	IndexJob

	srcTree *IndexTree
	dstTree *IndexTree
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
		srcTree: NewIndexTree(src),
		dstTree: NewIndexTree(dst),
	}
	si.SyncJob.SourceFilter(si.srcTree.IsNotIndex)
	si.SyncJob.DeleteFilter(si.dstTree.IsNotIndex)
	return si
}

func Sync(ctx context.Context, src, dst *storage.Bucket) error {
	return NewSyncIndexJob(src, dst).Do(ctx)
}

// SourceFilter selects which objects to copy from Source.
func (si *SyncIndexJob) SourceFilter(f storage.Filter) {
	si.SyncJob.SourceFilter(func(obj *gs.Object) bool {
		return f(obj) && si.srcTree.IsNotIndex(obj)
	})
}

// DeleteFilter selects which objects may be pruned from Destination.
func (si *SyncIndexJob) DeleteFilter(f storage.Filter) {
	si.SyncJob.DeleteFilter(func(obj *gs.Object) bool {
		return f(obj) && si.dstTree.IsNotIndex(obj)
	})
}

func (sj *SyncIndexJob) Do(ctx context.Context) error {
	if err := sj.SyncJob.Do(ctx); err != nil {
		return err
	}
	return sj.IndexJob.Do(ctx)
}
