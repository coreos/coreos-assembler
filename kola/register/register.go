// Copyright 2015 CoreOS, Inc.
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

package register

import (
	"errors"
	"fmt"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/go-semver/semver"
	"github.com/coreos/mantle/platform"
)

// Skip is a sentinel value that can be returned by tests that are skipped
// rather than passing or failing.
var Skip = errors.New("test skipped")

// Test provides the main test abstraction for kola. The run function is
// the actual testing function while the other fields provide ways to
// statically declare state of the platform.TestCluster before the test
// function is run.
type Test struct {
	Name        string // should be uppercase and unique
	Run         func(platform.TestCluster) error
	NativeFuncs map[string]func() error
	UserData    string
	ClusterSize int
	Platforms   []string // whitelist of platforms to run test against -- defaults to all

	// If manual is set, the test will only execute if the name fully matches without globbing.
	Manual bool

	// MinVersion prevents the test from executing on CoreOS machines
	// less than MinVersion. This will be ignored if the name fully
	// matches without globbing.
	MinVersion semver.Version

	// EndVersion prevents the test from executing on CoreOS machines
	// greater than or equal to EndVersion. This will be ignored if
	// the name fully matches without globbing.
	EndVersion semver.Version
}

// Registered tests live here. Mapping of names to tests.
var Tests = map[string]*Test{}

// Register is usually called in init() functions and is how kola test
// harnesses knows which tests it can choose from. Panics if existing
// name is registered
func Register(t *Test) {
	_, ok := Tests[t.Name]
	if ok {
		panic(fmt.Sprintf("test %v already registered", t.Name))
	}

	if (t.EndVersion != semver.Version{}) && !t.MinVersion.LessThan(t.EndVersion) {
		panic(fmt.Sprintf("test %v has an invalid version range", t.Name))
	}

	Tests[t.Name] = t
}
