// Copyright 2017 CoreOS, Inc.
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
	"bytes"
	"testing"
	"time"
)

const slack = time.Duration(500) * time.Millisecond

func TestTimeoutBasic(t *testing.T) {
	timeToRun := time.Duration(2) * time.Second

	tests := make(Tests)
	tests.Add("test-1", func(h *H) {
		h.StartExecTimer()
		defer h.StopExecTimer()
		select {
		case <-time.After(3 * time.Second):
			return
		case <-h.timeoutContext.Done():
			return
		}
	}, timeToRun)

	suite := NewSuite(Options{Parallel: 1}, tests)

	start := time.Now()
	buf := &bytes.Buffer{}
	if err := suite.runTests(buf, nil); err != nil {
		t.Log("\n" + buf.String())
	}

	total := time.Since(start)
	// We will make sure there are no goroutines leftover
	// In the kola tests, we check for timeouts in the SSH code
	// to exit running goroutines

	if !(timeToRun-slack < total && total < timeToRun+slack) {
		t.Errorf("Expected: %v +/- %v, Got: %v", timeToRun, slack, total)
	}

}

func TestTimeoutNoInterrupt(t *testing.T) {
	timeToRun := time.Duration(1) * time.Second

	tests := make(Tests)
	tests.Add("test-1", func(h *H) {
		h.StartExecTimer()
		defer h.StopExecTimer()
		select {
		case <-time.After(timeToRun):
			return
		case <-h.timeoutContext.Done():
			return
		}
	}, time.Duration(5)*time.Second)

	suite := NewSuite(Options{Parallel: 1}, tests)

	start := time.Now()
	buf := &bytes.Buffer{}
	if err := suite.runTests(buf, nil); err != nil {
		t.Log("\n" + buf.String())
	}
	total := time.Since(start)

	if !(timeToRun-slack < total && total < timeToRun+slack) {
		t.Errorf("Expected: %v +/- %v, Got: %v", timeToRun, slack, total)
	}
}

func TestTimeoutMultipleTests(t *testing.T) {
	totalTime := time.Duration(2) * time.Second

	tests := make(Tests)
	// Will finish after 1s
	tests.Add("test-1", func(h *H) {
		h.StartExecTimer()
		defer h.StopExecTimer()
		select {
		case <-time.After(1 * time.Second):
			return
		case <-h.timeoutContext.Done():
			return
		}
	}, time.Duration(5)*time.Second)

	// Will timeout after 1s
	tests.Add("test-2", func(h *H) {
		h.StartExecTimer()
		defer h.StopExecTimer()
		select {
		case <-time.After(2 * time.Second):
			return
		case <-h.timeoutContext.Done():
			return
		}
	}, time.Duration(1)*time.Second)

	suite := NewSuite(Options{Parallel: 2}, tests)

	start := time.Now()
	buf := &bytes.Buffer{}
	if err := suite.runTests(buf, nil); err != nil {
		t.Log("\n" + buf.String())
	}
	total := time.Since(start)

	if !(totalTime-slack < total && total < totalTime+slack) {
		t.Errorf("Expected: %v +/- %v, Got: %v", totalTime, slack, total)
	}
}
