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

package storage

import (
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/net/context"
	gs "github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/lang/worker"
)

type SyncJob struct {
	Source      *Bucket
	Destination *Bucket
}

func Sync(ctx context.Context, src, dst *Bucket) error {
	job := SyncJob{Source: src, Destination: dst}
	return job.Do(ctx)
}

func (sj *SyncJob) Do(ctx context.Context) error {
	// Assemble a set of existing objects which may be deleted.
	oldNames := make(map[string]struct{})
	for _, oldObj := range sj.Destination.Objects() {
		oldNames[oldObj.Name] = struct{}{}
	}

	wg := worker.NewWorkerGroup(ctx, MaxConcurrentRequests)
	for _, srcObj := range sj.Source.Objects() {
		obj := srcObj // for the sake of the closure
		name := sj.newName(srcObj)

		worker := func(c context.Context) error {
			return sj.Destination.Copy(c, obj, name)
		}
		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}

		// Drop from set of deletion candidates.
		delete(oldNames, name)
	}

	for oldName := range oldNames {
		name := oldName // for the sake of the closure
		worker := func(c context.Context) error {
			return sj.Destination.Delete(c, name)
		}
		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}
	}

	return wg.Wait()
}

func (sj *SyncJob) newName(srcObj *gs.Object) string {
	return sj.Destination.AddPrefix(
		sj.Source.TrimPrefix(srcObj.Name))
}
