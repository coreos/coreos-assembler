// Copyright 2017 CoreOS, Inc.
// Copyright 2014 The Go Authors.
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

package harness

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	g0 := runtime.NumGoroutine()

	code := m.Run()
	if code != 0 {
		os.Exit(code)
	}

	// Check that there are no goroutines left behind.
	t0 := time.Now()
	stacks := make([]byte, 1<<20)
	for {
		g1 := runtime.NumGoroutine()
		if g1 == g0 {
			return
		}
		stacks = stacks[:runtime.Stack(stacks, true)]
		time.Sleep(50 * time.Millisecond)
		if time.Since(t0) > 2*time.Second {
			fmt.Fprintf(os.Stderr, "Unexpected leftover goroutines detected: %v -> %v\n%s\n", g0, g1, stacks)
			os.Exit(1)
		}
	}
}

func TestContextCancel(t *testing.T) {
	suite := NewSuite([]InternalTest{
		{"ContextCancel", func(h *H) {
			ctx := h.Context()
			// Tests we don't leak this goroutine:
			go func() {
				<-ctx.Done()
			}()
		}}})
	r := suite.Run()
	if r != 0 {
		t.Errorf("Run failed: %d", r)
	}
}
