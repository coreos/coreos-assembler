// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package harness provides a reusable test framework akin to the standard
// "testing" Go package. For now there is no automated code generation
// component like the "go test" command but that may be a future extension.
// Test functions must be of type `func(*harness.H)` and registered directly
// with a test Suite struct which can then be launched via the Run method.
//
// Within these functions, use the Error, Fail or related methods to signal failure.
//
// Tests may be skipped if not applicable with a call to
// the Skip method of *H:
//     func NeedsSomeData(h *harness.H) {
//         if os.Getenv("SOME_DATA") == "" {
//             h.Skip("skipping test due to missing SOME_DATA")
//         }
//         ...
//     }
//
// Subtests
//
// The Run method of H allow defining subtests,
// without having to define separate functions for each. This enables uses
// like table-driven and hierarchical tests.
// It also provides a way to share common setup and tear-down code:
//
//     func Foo(h *harness.H) {
//         // <setup code>
//         h.Run("A=1", func(h *harness.H) { ... })
//         h.Run("A=2", func(h *harness.H) { ... })
//         h.Run("B=1", func(h *harness.H) { ... })
//         // <tear-down code>
//     }
//
// Each subtest has a unique name: the combination of the name
// of the top-level test and the sequence of names passed to Run, separated by
// slashes, with an optional trailing sequence number for disambiguation.
//
// The argument to the -harness.run command-line flag is an unanchored regular
// expression that matches the test's name. For tests with multiple slash-separated
// elements, such as subtests, the argument is itself slash-separated, with
// expressions matching each name element in turn. Because it is unanchored, an
// empty expression matches any string.
// For example, using "matching" to mean "whose name contains":
//
//     go run foo.go -harness.run ''      # Run all tests.
//     go run foo.go -harness.run Foo     # Run top-level tests matching "Foo", such as "TestFooBar".
//     go run foo.go -harness.run Foo/A=  # For top-level tests matching "Foo", run subtests matching "A=".
//     go run foo.go -harness.run /A=1    # For all top-level tests, run subtests matching "A=1".
//
// Subtests can also be used to control parallelism. A parent test will only
// complete once all of its subtests complete. In this example, all tests are
// run in parallel with each other, and only with each other, regardless of
// other top-level tests that may be defined:
//
//     func GroupedParallel(h *harness.H) {
//         for _, tc := range tests {
//             tc := tc // capture range variable
//             h.Run(tc.Name, func(h *harness.H) {
//                 h.Parallel()
//                 ...
//             })
//         }
//     }
//
// Run does not return until parallel subtests have completed, providing a way
// to clean up after a group of parallel tests:
//
//     func TeardownParallel(h *harness.H) {
//         // This Run will not return until the parallel tests finish.
//         h.Run("group", func(h *harness.H) {
//             h.Run("Test1", parallelTest1)
//             h.Run("Test2", parallelTest2)
//             h.Run("Test3", parallelTest3)
//         })
//         // <tear-down code>
//     }
//
// Suite
//
// Individual tests are grouped into a test suite in order to execute them.
// TODO: this part of the API deviates from the "testing" package and is TBD.
//
// A simple implementation of a test suite:
//
//	func SomeTest(h *harness.H) {
//		h.Skip("TODO")
//	}
//
//	func main() {
//		suite := harness.NewSuite([]InternalTest{
//			{"SomeTest", SomeTest},
//		})
//		os.Exit(suite.Run())
//	}
//
package harness

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

var (
	// The directory in which to create profile files and the like. When run from
	// "go test", the binary always runs in the source directory for the package;
	// this flag lets "go test" tell the binary to write the files in the directory where
	// the "go test" command is run.
	outputDir = flag.String("harness.outputdir", "", "write profiles to `dir`")

	// Report as tests are run; default is silent for success.
	chatty           = flag.Bool("harness.v", false, "verbose: print additional output")
	match            = flag.String("harness.run", "", "run only tests matching `regexp`")
	memProfile       = flag.String("harness.memprofile", "", "write a memory profile to `file`")
	memProfileRate   = flag.Int("harness.memprofilerate", 0, "set memory profiling `rate` (see runtime.MemProfileRate)")
	cpuProfile       = flag.String("harness.cpuprofile", "", "write a cpu profile to `file`")
	blockProfile     = flag.String("harness.blockprofile", "", "write a goroutine blocking profile to `file`")
	blockProfileRate = flag.Int("harness.blockprofilerate", 1, "set blocking profile `rate` (see runtime.SetBlockProfileRate)")
	traceFile        = flag.String("harness.trace", "", "write an execution trace to `file`")
	timeout          = flag.Duration("harness.timeout", 0, "fail test binary execution after duration `d` (0 means unlimited)")
	parallel         = flag.Int("harness.parallel", runtime.GOMAXPROCS(0), "run at most `n` tests in parallel")
)

// H is a type passed to Test functions to manage test state and support formatted test logs.
// Logs are accumulated during execution and dumped to standard output when done.
//
// A test ends when its Test function returns or calls any of the methods
// FailNow, Fatal, Fatalf, SkipNow, Skip, or Skipf. Those methods, as well as
// the Parallel method, must be called only from the goroutine running the
// Test function.
//
// The other reporting methods, such as the variations of Log and Error,
// may be called simultaneously from multiple goroutines.
type H struct {
	mu       sync.RWMutex // guards output, failed, and done.
	output   bytes.Buffer // Output generated by test.
	w        io.Writer    // For flushToParent.
	logger   *log.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	chatty   bool // A copy of the chatty flag.
	ran      bool // Test (or one of its subtests) was executed.
	failed   bool // Test has failed.
	skipped  bool // Test has been skipped.
	finished bool // Test function has completed.
	done     bool // Test is finished and all subtests have completed.
	hasSub   bool

	parent   *H
	level    int       // Nesting depth of test.
	name     string    // Name of test.
	start    time.Time // Time test started
	duration time.Duration
	barrier  chan bool // To signal parallel subtests they may start.
	signal   chan bool // To signal a test is done.
	sub      []*H      // Queue of subtests to be run in parallel.

	isParallel bool
	context    *testContext // For running tests and subtests.
}

func (c *H) parentContext() context.Context {
	if c == nil || c.parent == nil || c.parent.ctx == nil {
		return context.Background()
	}
	return c.parent.ctx
}

// Verbose reports whether the -harness.v flag is set.
func (h *H) Verbose() bool {
	return h.chatty
}

// flushToParent writes c.output to the parent after first writing the header
// with the given format and arguments.
func (c *H) flushToParent(format string, args ...interface{}) {
	p := c.parent
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Fprintf(p.w, format, args...)

	c.mu.Lock()
	defer c.mu.Unlock()
	io.Copy(p.w, &c.output)
}

type indenter struct {
	c *H
}

func (w indenter) Write(b []byte) (n int, err error) {
	n = len(b)
	for len(b) > 0 {
		end := bytes.IndexByte(b, '\n')
		if end == -1 {
			end = len(b)
		} else {
			end++
		}
		// An indent of 4 spaces will neatly align the dashes with the status
		// indicator of the parent.
		const indent = "    "
		w.c.output.WriteString(indent)
		w.c.output.Write(b[:end])
		b = b[end:]
	}
	return
}

// fmtDuration returns a string representing d in the form "87.00s".
func fmtDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// Name returns the name of the running test or benchmark.
func (c *H) Name() string {
	return c.name
}

// Context returns the context for the current test.
// The context is cancelled when the test finishes.
// A goroutine started during a test can wait for the
// context's Done channel to become readable as a signal that the
// test is over, so that the goroutine can exit.
func (c *H) Context() context.Context {
	return c.ctx
}

func (c *H) setRan() {
	if c.parent != nil {
		c.parent.setRan()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ran = true
}

// Fail marks the function as having failed but continues execution.
func (c *H) Fail() {
	if c.parent != nil {
		c.parent.Fail()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// c.done needs to be locked to synchronize checks to c.done in parent tests.
	if c.done {
		panic("Fail in goroutine after " + c.name + " has completed")
	}
	c.failed = true
}

// Failed reports whether the function has failed.
func (c *H) Failed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.failed
}

// FailNow marks the function as having failed and stops its execution.
// Execution will continue at the next test.
// FailNow must be called from the goroutine running the
// test function, not from other goroutines
// created during the test. Calling FailNow does not stop
// those other goroutines.
func (c *H) FailNow() {
	c.Fail()

	// Calling runtime.Goexit will exit the goroutine, which
	// will run the deferred functions in this goroutine,
	// which will eventually run the deferred lines in tRunner,
	// which will signal to the test loop that this test is done.
	//
	// A previous version of this code said:
	//
	//	c.duration = ...
	//	c.signal <- c.self
	//	runtime.Goexit()
	//
	// This previous version duplicated code (those lines are in
	// tRunner no matter what), but worse the goroutine teardown
	// implicit in runtime.Goexit was not guaranteed to complete
	// before the test exited. If a test deferred an important cleanup
	// function (like removing temporary files), there was no guarantee
	// it would run on a test failure. Because we send on c.signal during
	// a top-of-stack deferred function now, we know that the send
	// only happens after any other stacked defers have completed.
	c.finished = true
	runtime.Goexit()
}

// log generates the output. It's always at the same stack depth.
func (c *H) log(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logger.Output(3, s)
}

// Log formats its arguments using default formatting, analogous to Println,
// and records the text in the error log. The text will be printed only if
// the test fails or the -harness.v flag is set.
func (c *H) Log(args ...interface{}) { c.log(fmt.Sprintln(args...)) }

// Logf formats its arguments according to the format, analogous to Printf, and
// records the text in the error log. A final newline is added if not provided.
// The text will be printed only if the test fails or the -harness.v flag is set.
func (c *H) Logf(format string, args ...interface{}) { c.log(fmt.Sprintf(format, args...)) }

// Error is equivalent to Log followed by Fail.
func (c *H) Error(args ...interface{}) {
	c.log(fmt.Sprintln(args...))
	c.Fail()
}

// Errorf is equivalent to Logf followed by Fail.
func (c *H) Errorf(format string, args ...interface{}) {
	c.log(fmt.Sprintf(format, args...))
	c.Fail()
}

// Fatal is equivalent to Log followed by FailNow.
func (c *H) Fatal(args ...interface{}) {
	c.log(fmt.Sprintln(args...))
	c.FailNow()
}

// Fatalf is equivalent to Logf followed by FailNow.
func (c *H) Fatalf(format string, args ...interface{}) {
	c.log(fmt.Sprintf(format, args...))
	c.FailNow()
}

// Skip is equivalent to Log followed by SkipNow.
func (c *H) Skip(args ...interface{}) {
	c.log(fmt.Sprintln(args...))
	c.SkipNow()
}

// Skipf is equivalent to Logf followed by SkipNow.
func (c *H) Skipf(format string, args ...interface{}) {
	c.log(fmt.Sprintf(format, args...))
	c.SkipNow()
}

// SkipNow marks the test as having been skipped and stops its execution.
// If a test fails (see Error, Errorf, Fail) and is then skipped,
// it is still considered to have failed.
// Execution will continue at the next test. See also FailNow.
// SkipNow must be called from the goroutine running the test, not from
// other goroutines created during the test. Calling SkipNow does not stop
// those other goroutines.
func (c *H) SkipNow() {
	c.skip()
	c.finished = true
	runtime.Goexit()
}

func (c *H) skip() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skipped = true
}

// Skipped reports whether the test was skipped.
func (c *H) Skipped() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.skipped
}

// Parallel signals that this test is to be run in parallel with (and only with)
// other parallel tests.
func (t *H) Parallel() {
	if t.isParallel {
		panic("testing: t.Parallel called multiple times")
	}
	t.isParallel = true

	// We don't want to include the time we spend waiting for serial tests
	// in the test duration. Record the elapsed time thus far and reset the
	// timer afterwards.
	t.duration += time.Since(t.start)

	// Add to the list of tests to be released by the parent.
	t.parent.sub = append(t.parent.sub, t)

	t.signal <- true   // Release calling test.
	<-t.parent.barrier // Wait for the parent test to complete.
	t.context.waitParallel()
	t.start = time.Now()
}

// An internal type but exported because it is cross-package; part of the implementation
// of the "go test" command.
type InternalTest struct {
	Name string
	F    func(*H)
}

func tRunner(t *H, fn func(t *H)) {
	t.ctx, t.cancel = context.WithCancel(t.parentContext())
	defer t.cancel()

	// When this goroutine is done, either because fn(t)
	// returned normally or because a test failure triggered
	// a call to runtime.Goexit, record the duration and send
	// a signal saying that the test is done.
	defer func() {
		t.duration += time.Now().Sub(t.start)
		// If the test panicked, print any test output before dying.
		err := recover()
		if !t.finished && err == nil {
			err = fmt.Errorf("test executed panic(nil) or runtime.Goexit")
		}
		if err != nil {
			t.Fail()
			t.report()
			panic(err)
		}

		if len(t.sub) > 0 {
			// Run parallel subtests.
			// Decrease the running count for this test.
			t.context.release()
			// Release the parallel subtests.
			close(t.barrier)
			// Wait for subtests to complete.
			for _, sub := range t.sub {
				<-sub.signal
			}
			if !t.isParallel {
				// Reacquire the count for sequential tests. See comment in Run.
				t.context.waitParallel()
			}
		} else if t.isParallel {
			// Only release the count for this test if it was run as a parallel
			// test. See comment in Run method.
			t.context.release()
		}
		t.report() // Report after all subtests have finished.

		// Do not lock t.done to allow race detector to detect race in case
		// the user does not appropriately synchronizes a goroutine.
		t.done = true
		if t.parent != nil && !t.hasSub {
			t.setRan()
		}
		t.signal <- true
	}()

	t.start = time.Now()
	fn(t)
	t.finished = true
}

// Run runs f as a subtest of t called name. It reports whether f succeeded.
// Run will block until all its parallel subtests have completed.
func (t *H) Run(name string, f func(t *H)) bool {
	t.hasSub = true
	testName, ok := t.context.match.fullName(t, name)
	if !ok {
		return true
	}
	t = &H{
		barrier: make(chan bool),
		signal:  make(chan bool),
		name:    testName,
		parent:  t,
		level:   t.level + 1,
		chatty:  t.chatty,
		context: t.context,
	}
	t.w = indenter{t}
	t.logger = log.New(&t.output, "\t", log.Lshortfile)

	if t.chatty {
		// Print directly to root's io.Writer so there is no delay.
		root := t.parent
		for ; root.parent != nil; root = root.parent {
		}
		fmt.Fprintf(root.w, "=== RUN   %s\n", t.name)
	}
	// Instead of reducing the running count of this test before calling the
	// tRunner and increasing it afterwards, we rely on tRunner keeping the
	// count correct. This ensures that a sequence of sequential tests runs
	// without being preempted, even when their parent is a parallel test. This
	// may especially reduce surprises if *parallel == 1.
	go tRunner(t, f)
	<-t.signal
	return !t.failed
}

// testContext holds all fields that are common to all tests. This includes
// synchronization primitives to run at most *parallel tests.
type testContext struct {
	match *matcher

	mu sync.Mutex

	// Channel used to signal tests that are ready to be run in parallel.
	startParallel chan bool

	// running is the number of tests currently running in parallel.
	// This does not include tests that are waiting for subtests to complete.
	running int

	// numWaiting is the number tests waiting to be run in parallel.
	numWaiting int

	// maxParallel is a copy of the parallel flag.
	maxParallel int
}

func newTestContext(maxParallel int, m *matcher) *testContext {
	return &testContext{
		match:         m,
		startParallel: make(chan bool),
		maxParallel:   maxParallel,
		running:       1, // Set the count to 1 for the main (sequential) test.
	}
}

func (c *testContext) waitParallel() {
	c.mu.Lock()
	if c.running < c.maxParallel {
		c.running++
		c.mu.Unlock()
		return
	}
	c.numWaiting++
	c.mu.Unlock()
	<-c.startParallel
}

func (c *testContext) release() {
	c.mu.Lock()
	if c.numWaiting == 0 {
		c.running--
		c.mu.Unlock()
		return
	}
	c.numWaiting--
	c.mu.Unlock()
	c.startParallel <- true // Pick a waiting test to be run.
}

// Suite is a type passed to a TestMain function to run the actual tests.
type Suite struct {
	tests []InternalTest
}

// NewSuite
func NewSuite(tests []InternalTest) *Suite {
	return &Suite{
		tests: tests,
	}
}

// Run runs the tests. It returns an exit code to pass to os.Exit.
func (m *Suite) Run() int {
	// TestMain may have already called flag.Parse.
	if !flag.Parsed() {
		flag.Parse()
	}

	m.before()
	startAlarm()
	testRan, testOk := runTests(m.tests)
	if !testRan {
		fmt.Fprintln(os.Stderr, "testing: warning: no tests to run")
	}
	if !testOk {
		fmt.Println("FAIL")
		m.after()
		return 1
	}

	fmt.Println("PASS")
	m.after()
	return 0
}

func (t *H) report() {
	if t.parent == nil {
		return
	}
	dstr := fmtDuration(t.duration)
	format := "--- %s: %s (%s)\n"
	if t.Failed() {
		t.flushToParent(format, "FAIL", t.name, dstr)
	} else if t.chatty {
		if t.Skipped() {
			t.flushToParent(format, "SKIP", t.name, dstr)
		} else {
			t.flushToParent(format, "PASS", t.name, dstr)
		}
	}
}

func runTests(tests []InternalTest) (ran, ok bool) {
	ok = true
	ctx := newTestContext(*parallel, newMatcher(*match, "-harness.run"))
	t := &H{
		signal:  make(chan bool),
		barrier: make(chan bool),
		w:       os.Stdout,
		chatty:  *chatty,
		context: ctx,
	}
	tRunner(t, func(t *H) {
		for _, test := range tests {
			t.Run(test.Name, test.F)
		}
		// Run catching the signal rather than the tRunner as a separate
		// goroutine to avoid adding a goroutine during the sequential
		// phase as this pollutes the stacktrace output when aborting.
		go func() { <-t.signal }()
	})
	return t.ran, !t.Failed()
}

// before runs before all testing.
func (m *Suite) before() {
	if *memProfileRate > 0 {
		runtime.MemProfileRate = *memProfileRate
	}
	if *cpuProfile != "" {
		f, err := os.Create(toOutputDir(*cpuProfile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "testing: %s", err)
			return
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "testing: can't start cpu profile: %s", err)
			f.Close()
			return
		}
		// Could save f so after can call f.Close; not worth the effort.
	}
	if *traceFile != "" {
		f, err := os.Create(toOutputDir(*traceFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "testing: %s", err)
			return
		}
		if err := trace.Start(f); err != nil {
			fmt.Fprintf(os.Stderr, "testing: can't start tracing: %s", err)
			f.Close()
			return
		}
		// Could save f so after can call f.Close; not worth the effort.
	}
	if *blockProfile != "" && *blockProfileRate >= 0 {
		runtime.SetBlockProfileRate(*blockProfileRate)
	}
}

// after runs after all testing.
func (m *Suite) after() {
	if *cpuProfile != "" {
		pprof.StopCPUProfile() // flushes profile to disk
	}
	if *traceFile != "" {
		trace.Stop() // flushes trace to disk
	}
	if *memProfile != "" {
		f, err := os.Create(toOutputDir(*memProfile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "testing: %s\n", err)
			os.Exit(2)
		}
		runtime.GC() // materialize all statistics
		if err = pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "testing: can't write %s: %s\n", *memProfile, err)
			os.Exit(2)
		}
		f.Close()
	}
	if *blockProfile != "" && *blockProfileRate >= 0 {
		f, err := os.Create(toOutputDir(*blockProfile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "testing: %s\n", err)
			os.Exit(2)
		}
		if err = pprof.Lookup("block").WriteTo(f, 0); err != nil {
			fmt.Fprintf(os.Stderr, "testing: can't write %s: %s\n", *blockProfile, err)
			os.Exit(2)
		}
		f.Close()
	}
}

// toOutputDir returns the file name relocated, if required, to outputDir.
// Simple implementation to avoid pulling in path/filepath.
func toOutputDir(path string) string {
	if *outputDir == "" || path == "" {
		return path
	}
	if runtime.GOOS == "windows" {
		// On Windows, it's clumsy, but we can be almost always correct
		// by just looking for a drive letter and a colon.
		// Absolute paths always have a drive letter (ignoring UNC).
		// Problem: if path == "C:A" and outputdir == "C:\Go" it's unclear
		// what to do, but even then path/filepath doesn't help.
		// TODO: Worth doing better? Probably not, because we're here only
		// under the management of go test.
		if len(path) >= 2 {
			letter, colon := path[0], path[1]
			if ('a' <= letter && letter <= 'z' || 'A' <= letter && letter <= 'Z') && colon == ':' {
				// If path starts with a drive letter we're stuck with it regardless.
				return path
			}
		}
	}
	if os.IsPathSeparator(path[0]) {
		return path
	}
	return fmt.Sprintf("%s%c%s", *outputDir, os.PathSeparator, path)
}

var timer *time.Timer

// startAlarm starts an alarm if requested.
func startAlarm() {
	if *timeout > 0 {
		timer = time.AfterFunc(*timeout, func() {
			debug.SetTraceback("all")
			panic(fmt.Sprintf("test timed out after %v", *timeout))
		})
	}
}

// stopAlarm turns off the alarm.
func stopAlarm() {
	if *timeout > 0 {
		timer.Stop()
	}
}
