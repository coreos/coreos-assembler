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
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/harness/reporters"
	"github.com/coreos/coreos-assembler/mantle/harness/testresult"
)

const (
	defaultOutputDir = "_harness_temp"
)

var (
	SuiteEmpty  = errors.New("harness: no tests to run")
	SuiteFailed = errors.New("harness: test suite failed")
)

// Options
type Options struct {
	// The temporary directory in which to write profile files, logs, etc.
	OutputDir string

	// Report as tests are run; default is silent for success.
	Verbose bool

	// Run only tests matching a regexp.
	Match string

	// Enable memory profiling.
	MemProfile     bool
	MemProfileRate int

	// Enable CPU profiling.
	CpuProfile bool

	// Enable goroutine block profiling.
	BlockProfile     bool
	BlockProfileRate int

	// Enable execution trace.
	ExecutionTrace bool

	// Panic Suite execution after a timeout (0 means unlimited).
	Timeout time.Duration

	// Limit number of tests to run in parallel (0 means GOMAXPROCS).
	Parallel int

	// Sharding splits tests across runners
	Sharding string

	Reporters reporters.Reporters
}

// FlagSet can be used to setup options via command line flags.
// An optional prefix can be prepended to each flag.
// Defaults can be specified prior to calling FlagSet.
func (o *Options) FlagSet(prefix string, errorHandling flag.ErrorHandling) *flag.FlagSet {
	o.init()
	name := strings.Trim(prefix, ".-")
	f := flag.NewFlagSet(name, errorHandling)
	f.StringVar(&o.OutputDir, prefix+"outputdir", o.OutputDir,
		"write profiles, logs, and other data to temporary `dir`")
	f.BoolVar(&o.Verbose, prefix+"v", o.Verbose,
		"verbose: print additional output")
	f.StringVar(&o.Match, prefix+"run", o.Match,
		"run only tests matching `regexp`")
	f.BoolVar(&o.MemProfile, prefix+"memprofile", o.MemProfile,
		"write a memory profile to 'dir/mem.prof'")
	f.IntVar(&o.MemProfileRate, prefix+"memprofilerate", o.MemProfileRate,
		"set memory profiling `rate` (see runtime.MemProfileRate)")
	f.BoolVar(&o.CpuProfile, prefix+"cpuprofile", o.CpuProfile,
		"write a cpu profile to 'dir/cpu.prof'")
	f.BoolVar(&o.BlockProfile, prefix+"blockprofile", o.BlockProfile,
		"write a goroutine blocking profile to 'dir/block.prof'")
	f.IntVar(&o.BlockProfileRate, prefix+"blockprofilerate", o.BlockProfileRate,
		"set blocking profile `rate` (see runtime.SetBlockProfileRate)")
	f.BoolVar(&o.ExecutionTrace, prefix+"trace", o.ExecutionTrace,
		"write an execution trace to 'dir/exec.trace'")
	f.DurationVar(&o.Timeout, prefix+"timeout", o.Timeout,
		"fail test binary execution after duration `d` (0 means unlimited)")
	f.IntVar(&o.Parallel, prefix+"parallel", o.Parallel,
		"run at most `n` tests in parallel")
	return f
}

// init fills in any default values that shouldn't be the zero value.
func (o *Options) init() {
	if o.OutputDir == "" {
		o.OutputDir = defaultOutputDir
	}
	if o.MemProfileRate < 1 {
		o.MemProfileRate = runtime.MemProfileRate
	}
	if o.BlockProfileRate < 1 {
		o.BlockProfileRate = 1
	}
	if o.Parallel < 1 {
		o.Parallel = runtime.GOMAXPROCS(0)
	}
}

// Suite is a type passed to a TestMain function to run the actual tests.
// Suite manages the execution of a set of test functions.
type Suite struct {
	opts  Options
	tests Tests
	match *matcher

	// mu protects the following fields which are used to manage
	// parallel test execution.
	mu sync.Mutex

	// Channel used to signal tests that are ready to be run in parallel.
	startParallel chan bool

	// running is the number of tests currently running in parallel.
	// This does not include tests that are waiting for subtests to complete.
	running int

	// waiting is the number tests waiting to be run in parallel.
	waiting int
}

func (c *Suite) waitParallel() {
	c.mu.Lock()
	if c.running < c.opts.Parallel {
		c.running++
		c.mu.Unlock()
		return
	}
	c.waiting++
	c.mu.Unlock()
	<-c.startParallel
}

func (c *Suite) release() {
	c.mu.Lock()
	if c.waiting == 0 {
		c.running--
		c.mu.Unlock()
		return
	}
	c.waiting--
	c.mu.Unlock()
	c.startParallel <- true // Pick a waiting test to be run.
}

// NewSuite creates a new test suite.
// All parameters in Options cannot be modified once given to Suite.
func NewSuite(opts Options, tests Tests) *Suite {
	opts.init()
	return &Suite{
		opts:          opts,
		tests:         tests,
		match:         newMatcher(opts.Match, "Match"),
		startParallel: make(chan bool),
	}
}

// Run runs the tests. Returns SuiteFailed for any test failure.
func (s *Suite) Run() (err error) {
	flushProfile := func(name string, f *os.File) {
		err2 := pprof.Lookup(name).WriteTo(f, 0)
		if err == nil && err2 != nil {
			err = fmt.Errorf("harness: can't write %s profile: %v", name, err2)
		}
		f.Close()
	}

	outputDir, err := CleanOutputDir(s.opts.OutputDir)
	if err != nil {
		return err
	}
	s.opts.OutputDir = outputDir

	tap, err := os.Create(s.outputPath("test.tap"))
	if err != nil {
		return err
	}
	defer tap.Close()
	if _, err := fmt.Fprintf(tap, "1..%d\n", len(s.tests)); err != nil {
		return err
	}

	reportDir := s.outputPath("reports")
	if err := os.Mkdir(reportDir, 0777); err != nil {
		return err
	}
	defer func() {
		if reportErr := s.opts.Reporters.Output(reportDir); reportErr != nil && err != nil {
			err = reportErr
		}
	}()

	if s.opts.MemProfile {
		runtime.MemProfileRate = s.opts.MemProfileRate
		f, err := os.Create(s.outputPath("mem.prof"))
		if err != nil {
			return err
		}
		defer func() {
			runtime.GC() // materialize all statistics
			flushProfile("heap", f)
		}()
	}
	if s.opts.BlockProfile {
		f, err := os.Create(s.outputPath("block.prof"))
		if err != nil {
			return err
		}
		runtime.SetBlockProfileRate(s.opts.BlockProfileRate)
		defer func() {
			runtime.SetBlockProfileRate(0) // stop profile
			flushProfile("block", f)
		}()
	}
	if s.opts.CpuProfile {
		f, err := os.Create(s.outputPath("cpu.prof"))
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return errors.Wrapf(err, "harness: can't start cpu profile")
		}
		defer pprof.StopCPUProfile() // flushes profile to disk
	}
	if s.opts.ExecutionTrace {
		f, err := os.Create(s.outputPath("exec.trace"))
		if err != nil {
			return err
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			return errors.Wrapf(err, "harness: can't start tacing")
		}
		defer trace.Stop() // flushes trace to disk
	}
	if s.opts.Timeout > 0 {
		timer := time.AfterFunc(s.opts.Timeout, func() {
			debug.SetTraceback("all")
			panic(fmt.Sprintf("harness: tests timed out after %v", s.opts.Timeout))
		})
		defer timer.Stop()
	}

	return s.runTests(os.Stdout, tap)
}

func (s *Suite) runTests(out, tap io.Writer) error {
	s.running = 1 // Set the count to 1 for the main (sequential) test.
	t := &H{
		signal:    make(chan bool),
		barrier:   make(chan bool),
		w:         out,
		tap:       tap,
		suite:     s,
		reporters: s.opts.Reporters,
		// we set an initial non-zero timeout, this will
		// be overriden since the suite will run tests as subtests
		timeout: defaultTimeout,
	}
	tRunner(t, func(t *H) {
		for name, htest := range s.tests {
			t.RunTimeout(name, htest.run, htest.timeout)
		}
		// Run catching the signal rather than the tRunner as a separate
		// goroutine to avoid adding a goroutine during the sequential
		// phase as this pollutes the stacktrace output when aborting.
		go func() { <-t.signal }()
	})
	if !t.ran {
		if s.opts.Sharding != "" {
			fmt.Printf("notice: sharding %s enabled, no tests matched\n", s.opts.Sharding)
		} else {
			return SuiteEmpty
		}
	}
	if t.Failed() {
		s.opts.Reporters.SetResult(testresult.Fail)
		return SuiteFailed
	}

	s.opts.Reporters.SetResult(testresult.Pass)

	return nil
}

// outputPath returns the file name under Options.OutputDir.
func (s *Suite) outputPath(path string) string {
	return filepath.Join(s.opts.OutputDir, path)
}
