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
//		suite := harness.NewSuite(Options{}, Tests{
//			"SomeTest": SomeTest,
//		})
//		if err := suite.Run(); err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			fmt.Println("FAIL")
//			os.Exit(1)
//		}
//		fmt.Println("PASS")
//	}
//
package harness
