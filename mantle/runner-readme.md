# Kola Runner

## Table of Contents

 * [General Function Overview](#general-function-overview)
 * [Visual Function Flow](#visual-function-flow)
 * [Detailed Workflow](#detailed-workflow)
 * [Common Closures](#common-closures)
 * [Logging](#logging)

The `kola` runner is very similar to the standard `go test` runner with some
modifications to allow tighter control over the run loop and extensibility
for logging (without parsing) & output control.

## General Function Overview

### kola/harness

#### kola/harness: RunTests

Responsible for filtering down the test list based on the given criteria,
creating the `harness/suite: Suite` object, and outputting the final result.

#### kola/harness: runTest

Creates the test cluster, runs the individual test function, and cleans up the
test cluster.

### harness/suite

#### harness/suite: Run

Creates the output directory, the `test.tap` file and any profile related
files then calls `harness/suite: runTests`.

#### harness/suite: runTests

Sets up the `harness/harness: H` object and calls `harness/harness: tRunner`
with a closure to call `harness/harness: Run` for each test.

### harness/harness

#### harness/harness: tRunner

Responsible for the timing, reporting, and execution of a closure.

#### harness/harness: Run

Handles the setup of child `harness/harness: H` objects, loggers, and the
running of closures as subtests.

## Visual Function Flow

The first 4 steps handle filtering down the test list, creating the
clusters, and building the suite. The next 4 set up the reporting
structure of the test group and run the child tests. The following 2 get
ready to run each individual test. And the final step runs the actual
test function registered in the test.

```
cmd/kola/kola
      |
      v
kola/harness: RunTests
      |
      v
harness/suite: Run
      |
      v
harness/suite: runTests
      |
      v
harness/harness: tRunner
      |
      v
harness/harness: Run
      |
      v
harness/harness: tRunner
      |
      v
kola/harness: runTest
      |
      v
harness/harness: Run
      |
      v
harness/harness: tRunner
      |
      v
kola/register/register: Run
```

## Detailed Workflow

1. The `kola` cmd calls into `kola/harness: RunTests`
2. `kola/harness: RunTests` calls `kola/harness: filterTests` to build a test
list filtered down by the given pattern & platform from all tests in
`kola/register/register: Tests` object.
3. `kola/harness: RunTests` will then construct a `harness/suite: Options`
object and construct a `harness/test: Test` object containing the name of each
test and a closure (#1) calling `kola/harness: runTest`.
4. `kola/harness: RunTests` constructs a `harness/suite: Suite` object via
`harness/suite: NewSuite` using the `harness/suite: Options` and
`harness/test: Test` objects and proceeds to call the `harness/suite: Run`
function on the `harness/suite: Suite` object.
5. `harness/suite: Run` starts by creating or cleaning up the output directory
by calling the `harness/harness: CleanOutputDir` function. It then creates the
`test.tap` file inside of the output directory and prints a string to the file
containing `1..%d` where %d is the amount of tests being run.
6. `harness/suite: Run` then checks if the following options were selected and
if so creates the corresponding files in the output path:

| Option         | Filename   |
| -------------- | ---------- |
| MemProfile     | mem.prof   |
| BlockProfile   | block.prof |
| CpuProfile     | cpu.prof   |
| ExecutionTrace | exec.trace |

7. `harness/suite: Run` then calls `harness/suite: runTests` passing
`os.Stdout` and the `tap io.Writer` object.
8. `harness/suite: runTests` starts by setting the `running` variable on the
`harness/suite: Suite` object, which is the count of running tests, to 1 and
creating the `harness/harness: H` object.
9. `harness/suite: runTests` then calls `harness/harness: tRunner` passing
the `harness/harness: H` object and a closure (#2) which loops each test in the
`harness/suite: Suite` object calling `harness/harness: Run` on each, passing
the name of the test, the `harness/test: Test` object, and a boolean pointer
set to false, followed by a goroutine call to receive from the signal channel
on the `harness/harness: H` object.
10. `harness/harness: tRunner` starts by creating a `context.WithCancel`
object, the result of `harness/harness: parentContext` is passed in which will
either be the context object of the `harness/harness: H` objects parent or
`context.Background()` if the object doesn't have a parent.
11. `harness/harness: tRunner` then defers a closure which will detect the
status of the test run, calculate the ending time, run any subtests, call
`harness/harness: report` (which will flush the test result to the parent
via the `harness/harness: flushToParent` function), and send `true` on the
`harness/harness: H` `signal` channel.
12. `harness/harness: tRunner` will then calculate the start time and call the
closure it received as an argument with the `harness/harness: H` variable as a
parameter, this will be the closure that was created in
`harness/suite: runTests` which will call `harness/harness: Run` for each test.
13. `harness/harness: Run` runs each function as a subtest of the
`harness/harness: H` object it is passed with the name passed. It starts by
marking the `hasSub` variable on the `harness/harness: H` object to true and
checking that the test name it received is a valid test via the
`harness/match: fullName` function.
14. `harness/harness: Run` will then create a new `harness/harness: H` object
which has the object it received as the parent and a `log` object.
15. `harness/harness: Run` then does a goroutine call on
`harness/harness: tRunner` passing in the new `harness/harness: H` object,
the closure function it was passed, which is the call to
`kola/harness: runTest`, and the boolean pointer it was passed.
16. `harness/harness: tRunner` will then run through and call
`kola/harness: runTest`.
17. `kola/harness: runTest` is the harness responsible for running a single
test grouping (test groupings tests. It will create the cluster that
will be used by the tests, validate that the machines spun up properly,
and then call `kola/register/register: Run` on the
`kola/register/register: Test` object, which is a function pointer which
accepts a `kola/cluster/cluster: TestCluster` object and is defined
inside of the individual test files.

## Common Closures

1. `kola/harness: RunTests`

Accepts a `harness/harness: H` object and calls `kola/harness: runTest`

```
func(h *harness.H) {
        runTest(h, []*register.Test{test}, pltfrm, false)
}
```

2. `harness/suite: runTests`

Accepts a `harness/harness: H` object. Loops each test in the
`harness/suite: Suite` object calling `harness/harness: Run`. This is being
pass as an argument to `harness/harness: tRunner`. `harness/harness:tRunner`
will time the the outer block and call `harness/harness: Run` which will run
the `test` function as a subtest.

For instance, `harness/harness: tRunner` will be called with the
`harness/harness: H` object representing the entire test run. It will then
execute this closure which will loop through every test and call
`harness/harness: Run` which will run each as a subtest for reporting purposes
inside of goroutines.

```
func(t *H) {
        for name, test := range s.tests {
                t.Run(name, test, util.BoolToPtr(false))
        }
        // Run catching the signal rather than the tRunner as a separate
        // goroutine to avoid adding a goroutine during the sequential
        // phase as this pollutes the stacktrace output when aborting.
        go func() { <-t.signal }()
}
```

## Logging

The `kola` runner supports custom reporting via the
`harness/reporters: Reporter` interface. By default plain text will be output
into `stdout` and a JSON file will be produced inside of the `_kola_temp` run
log (e.x.: `_kola_temp/<platform>-latest/reports/report.json`). New output
formats can be added by creating a new struct which implements the
`harness/reporters: Reporter` interface and instantiating an object of said
reporter inside of the `harness: Options` object created in
`kola/harness: RunTests`.

[For example](https://github.com/coreos/mantle/blob/52407c3ae8cd0837511c665af2c7870393e024bb/kola/harness.go#L295-L297) this is how the JSON reporter is added.
