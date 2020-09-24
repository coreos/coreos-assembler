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
	"fmt"

	"github.com/coreos/mantle/lang/maps"
)

// Test is a single test function.
type Test func(*H)

// Tests is a set of test functions that can be given to a Suite.
type Tests map[string]Test

// Add inserts the given Test into the set, initializing Tests if needed.
// If a test with the given name already exists Add will panic.
func (ts *Tests) Add(name string, test Test) {
	if *ts == nil {
		*ts = make(Tests)
	} else if _, ok := (*ts)[name]; ok {
		panic(fmt.Errorf("harness: duplicate test %q", name))
	}
	(*ts)[name] = test
}

// List returns a sorted list of test names.
func (ts Tests) List() []string {
	return maps.NaturalKeys(ts)
}
