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
	"sync"

	"github.com/coreos/pkg/multierror"
	"golang.org/x/net/context"
)

// Worker is a function that WorkerGroup will run in a new goroutine.
type Worker func(context.Context) error

// WorkerGroup is similar in principle to sync.WaitGroup but manages the
// Workers itself. This allows it to provide a few helpful features:
//  - Integration with the context library.
//  - Limit the number of concurrent Workers.
//  - Capture the errors returned by each Worker.
//  - Abort everything after a single Worker reports an error.
type WorkerGroup struct {
	ctx    context.Context
	cancel context.CancelFunc
	limit  chan struct{}

	mu     sync.Mutex
	errors multierror.Error
}

// NewWorkerGroup creates a new group.
func NewWorkerGroup(ctx context.Context, workerLimit int) *WorkerGroup {
	wg := WorkerGroup{limit: make(chan struct{}, workerLimit)}
	wg.ctx, wg.cancel = context.WithCancel(ctx)
	return &wg
}

func (wg *WorkerGroup) addErr(err error) {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	wg.errors = append(wg.errors, err)
	wg.cancel()
}

func (wg *WorkerGroup) getErr() error {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	return wg.errors.AsError()
}

// Start launches a new worker, blocking if too many workers are
// already running. An error indicates the group's context is closed.
func (wg *WorkerGroup) Start(worker Worker) error {
	// check for cancellation before waiting on a worker slot
	select {
	default:
	case <-wg.ctx.Done():
		return wg.ctx.Err()
	}
	select {
	case wg.limit <- struct{}{}:
		go func() {
			if err := worker(wg.ctx); err != nil {
				wg.addErr(err)
			}
			<-wg.limit
		}()
		return nil
	case <-wg.ctx.Done():
		return wg.ctx.Err()
	}
}

// Wait blocks until all running workers have finished. An error
// indicates if at least one worker returned an error or was canceled.
func (wg *WorkerGroup) Wait() error {
	// make sure cancel is called at least once, after all work is done.
	defer wg.cancel()
	for i := 0; i < cap(wg.limit); i++ {
		wg.limit <- struct{}{}
	}
	return wg.getErr()
}

// Wait with a default error value that will be returned if no worker failed.
//
//	if err := wg.Start(worker); err != nil {
//		return wg.WaitError(err)
//	}
//
func (wg *WorkerGroup) WaitError(err error) error {
	if werr := wg.Wait(); werr != nil {
		return werr
	}
	return err
}
