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

func Index(ctx context.Context, bucket *storage.Bucket) error {
	wg := worker.NewWorkerGroup(ctx, maxWorkers)
	tree := NewIndexTree(bucket)
	var doDir func(string) error
	doDir = func(dir string) error {
		ix := tree.Indexer(dir)
		if err := wg.Start(ix.UpdateRedirect); err != nil {
			return err
		}
		if err := wg.Start(ix.UpdateDirectoryHTML); err != nil {
			return err
		}
		if err := wg.Start(ix.UpdateIndexHTML); err != nil {
			return err
		}
		for _, subdir := range ix.SubDirs {
			if err := doDir(subdir); err != nil {
				return err
			}
		}
		return nil
	}

	if err := doDir(bucket.Prefix()); err != nil {
		return wg.WaitError(err)
	}

	for _, index := range tree.EmptyIndexes(bucket.Prefix()) {
		objName := index
		if err := wg.Start(func(ctx context.Context) error {
			return bucket.Delete(ctx, objName)
		}); err != nil {
			return wg.WaitError(err)
		}
	}

	return wg.Wait()
}
