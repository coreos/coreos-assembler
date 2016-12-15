// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package harness

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTestContext(t *testing.T) {
	const (
		add1 = 0
		done = 1
	)
	// After each of the calls are applied to the context, the
	type call struct {
		typ int // run or done
		// result from applying the call
		running int
		waiting int
		started bool
	}
	testCases := []struct {
		max int
		run []call
	}{{
		max: 1,
		run: []call{
			{typ: add1, running: 1, waiting: 0, started: true},
			{typ: done, running: 0, waiting: 0, started: false},
		},
	}, {
		max: 1,
		run: []call{
			{typ: add1, running: 1, waiting: 0, started: true},
			{typ: add1, running: 1, waiting: 1, started: false},
			{typ: done, running: 1, waiting: 0, started: true},
			{typ: done, running: 0, waiting: 0, started: false},
			{typ: add1, running: 1, waiting: 0, started: true},
		},
	}, {
		max: 3,
		run: []call{
			{typ: add1, running: 1, waiting: 0, started: true},
			{typ: add1, running: 2, waiting: 0, started: true},
			{typ: add1, running: 3, waiting: 0, started: true},
			{typ: add1, running: 3, waiting: 1, started: false},
			{typ: add1, running: 3, waiting: 2, started: false},
			{typ: add1, running: 3, waiting: 3, started: false},
			{typ: done, running: 3, waiting: 2, started: true},
			{typ: add1, running: 3, waiting: 3, started: false},
			{typ: done, running: 3, waiting: 2, started: true},
			{typ: done, running: 3, waiting: 1, started: true},
			{typ: done, running: 3, waiting: 0, started: true},
			{typ: done, running: 2, waiting: 0, started: false},
			{typ: done, running: 1, waiting: 0, started: false},
			{typ: done, running: 0, waiting: 0, started: false},
		},
	}}
	for i, tc := range testCases {
		ctx := &testContext{
			startParallel: make(chan bool),
			maxParallel:   tc.max,
		}
		for j, call := range tc.run {
			doCall := func(f func()) chan bool {
				done := make(chan bool)
				go func() {
					f()
					done <- true
				}()
				return done
			}
			started := false
			switch call.typ {
			case add1:
				signal := doCall(ctx.waitParallel)
				select {
				case <-signal:
					started = true
				case ctx.startParallel <- true:
					<-signal
				}
			case done:
				signal := doCall(ctx.release)
				select {
				case <-signal:
				case <-ctx.startParallel:
					started = true
					<-signal
				}
			}
			if started != call.started {
				t.Errorf("%d:%d:started: got %v; want %v", i, j, started, call.started)
			}
			if ctx.running != call.running {
				t.Errorf("%d:%d:running: got %v; want %v", i, j, ctx.running, call.running)
			}
			if ctx.numWaiting != call.waiting {
				t.Errorf("%d:%d:waiting: got %v; want %v", i, j, ctx.numWaiting, call.waiting)
			}
		}
	}
}

func TestTRun(t *testing.T) {
	realTest := t
	testCases := []struct {
		desc   string
		ok     bool
		maxPar int
		chatty bool
		output string
		f      func(*H)
	}{{
		desc:   "failnow skips future sequential and parallel tests at same level",
		ok:     false,
		maxPar: 1,
		output: `
--- FAIL: failnow skips future sequential and parallel tests at same level (N.NNs)
    --- FAIL: failnow skips future sequential and parallel tests at same level/#00 (N.NNs)
    `,
		f: func(t *H) {
			ranSeq := false
			ranPar := false
			t.Run("", func(t *H) {
				t.Run("par", func(t *H) {
					t.Parallel()
					ranPar = true
				})
				t.Run("seq", func(t *H) {
					ranSeq = true
				})
				t.FailNow()
				t.Run("seq", func(t *H) {
					realTest.Error("test must be skipped")
				})
				t.Run("par", func(t *H) {
					t.Parallel()
					realTest.Error("test must be skipped.")
				})
			})
			if !ranPar {
				realTest.Error("parallel test was not run")
			}
			if !ranSeq {
				realTest.Error("sequential test was not run")
			}
		},
	}, {
		desc:   "failure in parallel test propagates upwards",
		ok:     false,
		maxPar: 1,
		output: `
--- FAIL: failure in parallel test propagates upwards (N.NNs)
    --- FAIL: failure in parallel test propagates upwards/#00 (N.NNs)
        --- FAIL: failure in parallel test propagates upwards/#00/par (N.NNs)
		`,
		f: func(t *H) {
			t.Run("", func(t *H) {
				t.Parallel()
				t.Run("par", func(t *H) {
					t.Parallel()
					t.Fail()
				})
			})
		},
	}, {
		desc:   "skipping without message, chatty",
		ok:     true,
		chatty: true,
		output: `
=== RUN   skipping without message, chatty
--- SKIP: skipping without message, chatty (N.NNs)`,
		f: func(t *H) { t.SkipNow() },
	}, {
		desc:   "chatty with recursion",
		ok:     true,
		chatty: true,
		output: `
=== RUN   chatty with recursion
=== RUN   chatty with recursion/#00
=== RUN   chatty with recursion/#00/#00
--- PASS: chatty with recursion (N.NNs)
    --- PASS: chatty with recursion/#00 (N.NNs)
        --- PASS: chatty with recursion/#00/#00 (N.NNs)`,
		f: func(t *H) {
			t.Run("", func(t *H) {
				t.Run("", func(t *H) {})
			})
		},
	}, {
		desc: "skipping without message, not chatty",
		ok:   true,
		f:    func(t *H) { t.SkipNow() },
	}, {
		desc: "skipping after error",
		output: `
--- FAIL: skipping after error (N.NNs)
	sub_test.go:NNN: an error
	sub_test.go:NNN: skipped`,
		f: func(t *H) {
			t.Error("an error")
			t.Skip("skipped")
		},
	}, {
		desc:   "use Run to locally synchronize parallelism",
		ok:     true,
		maxPar: 1,
		f: func(t *H) {
			var count uint32
			t.Run("waitGroup", func(t *H) {
				for i := 0; i < 4; i++ {
					t.Run("par", func(t *H) {
						t.Parallel()
						atomic.AddUint32(&count, 1)
					})
				}
			})
			if count != 4 {
				t.Errorf("count was %d; want 4", count)
			}
		},
	}, {
		desc: "alternate sequential and parallel",
		// Sequential tests should partake in the counting of running threads.
		// Otherwise, if one runs parallel subtests in sequential tests that are
		// itself subtests of parallel tests, the counts can get askew.
		ok:     true,
		maxPar: 1,
		f: func(t *H) {
			t.Run("a", func(t *H) {
				t.Parallel()
				t.Run("b", func(t *H) {
					// Sequential: ensure running count is decremented.
					t.Run("c", func(t *H) {
						t.Parallel()
					})

				})
			})
		},
	}, {
		desc: "alternate sequential and parallel 2",
		// Sequential tests should partake in the counting of running threads.
		// Otherwise, if one runs parallel subtests in sequential tests that are
		// itself subtests of parallel tests, the counts can get askew.
		ok:     true,
		maxPar: 2,
		f: func(t *H) {
			for i := 0; i < 2; i++ {
				t.Run("a", func(t *H) {
					t.Parallel()
					time.Sleep(time.Nanosecond)
					for i := 0; i < 2; i++ {
						t.Run("b", func(t *H) {
							time.Sleep(time.Nanosecond)
							for i := 0; i < 2; i++ {
								t.Run("c", func(t *H) {
									t.Parallel()
									time.Sleep(time.Nanosecond)
								})
							}

						})
					}
				})
			}
		},
	}, {
		desc:   "stress test",
		ok:     true,
		maxPar: 4,
		f: func(t *H) {
			// t.Parallel doesn't work in the pseudo-H we start with:
			// it leaks a goroutine.
			// Call t.Run to get a real one.
			t.Run("X", func(t *H) {
				t.Parallel()
				for i := 0; i < 12; i++ {
					t.Run("a", func(t *H) {
						t.Parallel()
						time.Sleep(time.Nanosecond)
						for i := 0; i < 12; i++ {
							t.Run("b", func(t *H) {
								time.Sleep(time.Nanosecond)
								for i := 0; i < 12; i++ {
									t.Run("c", func(t *H) {
										t.Parallel()
										time.Sleep(time.Nanosecond)
										t.Run("d1", func(t *H) {})
										t.Run("d2", func(t *H) {})
										t.Run("d3", func(t *H) {})
										t.Run("d4", func(t *H) {})
									})
								}
							})
						}
					})
				}
			})
		},
	}, {
		desc:   "skip output",
		ok:     true,
		maxPar: 4,
		f: func(t *H) {
			t.Skip()
		},
	}, {
		desc:   "panic on goroutine fail after test exit",
		ok:     false,
		maxPar: 4,
		f: func(t *H) {
			ch := make(chan bool)
			t.Run("", func(t *H) {
				go func() {
					<-ch
					defer func() {
						if r := recover(); r == nil {
							realTest.Errorf("expected panic")
						}
						ch <- true
					}()
					t.Errorf("failed after success")
				}()
			})
			ch <- true
			<-ch
		},
	}}
	for _, tc := range testCases {
		ctx := newTestContext(tc.maxPar, newMatcher("", ""))
		buf := &bytes.Buffer{}
		root := &H{
			signal:  make(chan bool),
			name:    "Test",
			w:       buf,
			chatty:  tc.chatty,
			context: ctx,
		}
		root.ctx, root.cancel = context.WithCancel(context.Background())
		ok := root.Run(tc.desc, tc.f)
		ctx.release()

		if ok != tc.ok {
			t.Errorf("%s:ok: got %v; want %v", tc.desc, ok, tc.ok)
		}
		if ok != !root.Failed() {
			t.Errorf("%s:root failed: got %v; want %v", tc.desc, !ok, root.Failed())
		}
		if ctx.running != 0 || ctx.numWaiting != 0 {
			t.Errorf("%s:running and waiting non-zero: got %d and %d", tc.desc, ctx.running, ctx.numWaiting)
		}
		got := strings.TrimSpace(buf.String())
		want := strings.TrimSpace(tc.output)
		re := makeRegexp(want)
		if ok, err := regexp.MatchString(re, got); !ok || err != nil {
			t.Errorf("%s:ouput:\ngot:\n%s\nwant:\n%s", tc.desc, got, want)
		}
	}
}

func makeRegexp(s string) string {
	s = strings.Replace(s, ":NNN:", `:\d\d\d:`, -1)
	s = strings.Replace(s, "(N.NNs)", `\(\d*\.\d*s\)`, -1)
	return s
}
