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

	"github.com/coreos/mantle/lang/worker"
)

// Arbitrary limit on the number of concurrent jobs
const maxWorkers = 12

func Sync(ctx context.Context, src, dst *Bucket) error {
	wg := worker.NewWorkerGroup(ctx, maxWorkers)
	for _, srcObj := range src.Objects() {
		obj := srcObj // for the sake of the closure
		worker := func(c context.Context) error {
			name := dst.AddPrefix(src.TrimPrefix(obj.Name))
			return dst.Copy(c, obj, name)
		}
		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}
	}
	return wg.Wait()
}
