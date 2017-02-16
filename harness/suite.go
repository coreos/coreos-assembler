// Copyright 2017 CoreOS, Inc.
// Copyright 2009 The Go Authors.
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
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

var (
	SuiteEmpty  = errors.New("harness: no tests to run")
	SuiteFailed = errors.New("harness: test suite failed")

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

// An internal type but exported because it is cross-package; part of the implementation
// of the "go test" command.
type InternalTest struct {
	Name string
	F    func(*H)
}

// Suite is a type passed to a TestMain function to run the actual tests.
// Suite manages the execution of a set of test functions.
type Suite struct {
	tests  []InternalTest
	match  *matcher
	chatty bool

	// mu protects the following fields which are used to manage
	// parallel test execution.
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

func (c *Suite) waitParallel() {
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

func (c *Suite) release() {
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

// NewSuite
func NewSuite(tests []InternalTest) *Suite {
	return &Suite{
		tests:         tests,
		startParallel: make(chan bool),
	}
}

// Run runs the tests. Returns SuiteFailed for any test failure.
func (s *Suite) Run() error {
	// The user may have already called flag.Parse.
	if !flag.Parsed() {
		flag.Parse()
	}

	// Initialize Suite based on command line flags.
	// TODO(marineam): offer other ways to do this.
	s.match = newMatcher(*match, "-harness.run")
	s.maxParallel = *parallel
	s.chatty = *chatty
	s.running = 1 // Set the count to 1 for the main (sequential) test.
	s.before()
	startAlarm()
	err := s.runTests()
	s.after()
	return err
}

func (s *Suite) runTests() error {
	t := &H{
		signal:  make(chan bool),
		barrier: make(chan bool),
		w:       os.Stdout,
		suite:   s,
	}
	tRunner(t, func(t *H) {
		for _, test := range s.tests {
			t.Run(test.Name, test.F)
		}
		// Run catching the signal rather than the tRunner as a separate
		// goroutine to avoid adding a goroutine during the sequential
		// phase as this pollutes the stacktrace output when aborting.
		go func() { <-t.signal }()
	})
	if !t.ran {
		return SuiteEmpty
	}
	if t.Failed() {
		return SuiteFailed
	}
	return nil
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
