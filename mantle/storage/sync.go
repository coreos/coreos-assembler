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
	"strings"

	"golang.org/x/net/context"
	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/lang/worker"
)

// Filter is a type of function that returns true if an object should be
// included in a given operation or false if it should be excluded/ignored.
type Filter func(*gs.Object) bool

type SyncJob struct {
	Source      *Bucket
	Destination *Bucket

	sourcePrefix      *string
	destinationPrefix *string
	sourceFilter      Filter
	deleteFilter      Filter
	enableDelete      bool
	notRecursive      bool // inverted because recursive is default
}

func Sync(ctx context.Context, src, dst *Bucket) error {
	job := SyncJob{Source: src, Destination: dst}
	return job.Do(ctx)
}

// SourcePrefix overrides the Source bucket's default prefix.
func (sj *SyncJob) SourcePrefix(p string) {
	p = FixPrefix(p)
	sj.sourcePrefix = &p
}

// DestinationPrefix overrides the Destination bucket's default prefix.
func (sj *SyncJob) DestinationPrefix(p string) {
	p = FixPrefix(p)
	sj.destinationPrefix = &p
}

// SourceFilter selects which objects to copy from Source.
func (sj *SyncJob) SourceFilter(f Filter) {
	sj.sourceFilter = f
}

// DeleteFilter selects which objects may be pruned from Destination.
func (sj *SyncJob) DeleteFilter(f Filter) {
	sj.deleteFilter = f
}

// Delete toggles deletion of extra objects from Destination.
func (sj *SyncJob) Delete(enable bool) {
	sj.enableDelete = enable
}

// Recursive toggles copying subdirectories from Source (the default).
func (sj *SyncJob) Recursive(enable bool) {
	sj.notRecursive = !enable
}

func (sj *SyncJob) Do(ctx context.Context) error {
	if sj.sourcePrefix == nil {
		prefix := sj.Source.Prefix()
		sj.sourcePrefix = &prefix
	}
	if sj.destinationPrefix == nil {
		prefix := sj.Destination.Prefix()
		sj.destinationPrefix = &prefix
	}

	// Assemble a set of existing objects which may be deleted.
	oldNames := make(map[string]struct{})
	for _, oldObj := range sj.Destination.Objects() {
		if !sj.hasPrefix(oldObj.Name, *sj.destinationPrefix) {
			continue
		}
		if sj.deleteFilter != nil && !sj.deleteFilter(oldObj) {
			continue
		}
		oldNames[oldObj.Name] = struct{}{}
	}

	wg := worker.NewWorkerGroup(ctx, MaxConcurrentRequests)
	for _, srcObj := range sj.Source.Objects() {
		if !sj.hasPrefix(srcObj.Name, *sj.sourcePrefix) {
			continue
		}
		if sj.sourceFilter != nil && !sj.sourceFilter(srcObj) {
			continue
		}

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

func (sj *SyncJob) hasPrefix(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if sj.notRecursive {
		suffix := name[len(prefix):]
		if strings.Contains(suffix, "/") {
			return false
		}
	}
	return true
}

func (sj *SyncJob) newName(srcObj *gs.Object) string {
	return *sj.destinationPrefix + srcObj.Name[len(*sj.sourcePrefix):]
}
