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

// This example program illustrates how to create a custom test suite
// based on the harness package. main.go contains the test suite glue
// while tests.go contains example tests.
//
// The custom test suite adds a feature to give individual tests some
// data that can be overridden in the environment. When executed:
//
//	./example  -v
//	=== RUN   LogIt
//	--- PASS: LogIt (0.00s)
//	        tests.go:27: Got "something"
//	=== RUN   SkipIt
//	--- SKIP: SkipIt (0.00s)
//	        main.go:40: Missing "TEST_DATA_else" in environment.
//	PASS
//
package main
