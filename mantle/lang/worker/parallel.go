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

package worker

import (
	"golang.org/x/net/context"
)

// Parallel executes a set of Workers and waits for them to finish.
func Parallel(ctx context.Context, workers ...Worker) error {
	wg := NewWorkerGroup(ctx, len(workers))
	for _, worker := range workers {
		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}
	}
	return wg.Wait()
}
