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
	"fmt"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform/conf"
)

type Flag int

const (
	NoSSHKeyInUserData     Flag = iota // don't inject SSH key into Ignition/cloud-config
	NoSSHKeyInMetadata                 // don't add SSH key to platform metadata
	NoEmergencyShellCheck              // don't check console output for emergency shell invocation
	RequiresInternetAccess             // run the test only if the platform supports Internet access
)

// NativeFuncWrap is a wrapper for the NativeFunc which includes an optional string of arches and/or distributions to
// exclude for each native test.
type NativeFuncWrap struct {
	NativeFunc func() error
	Exclusions []string
}

// CreateNativeFuncWrap is a simple constructor for returning NativeFuncWrap structure.
// exclusions can be architectures and/or distributions.
func CreateNativeFuncWrap(f func() error, exclusions ...string) NativeFuncWrap {
	return NativeFuncWrap{f, exclusions}
}

// Test provides the main test abstraction for kola. The run function is
// the actual testing function while the other fields provide ways to
// statically declare state of the platform.TestCluster before the test
// function is run.
type Test struct {
	Name                 string // should be unique
	Run                  func(cluster.TestCluster)
	NativeFuncs          map[string]NativeFuncWrap
	UserData             *conf.UserData
	ClusterSize          int
	Platforms            []string // allowlist of platforms to run test against -- defaults to all
	Firmwares            []string // allowlist of firmwares to run test against -- defaults to all
	ExcludePlatforms     []string // denylist of platforms to ignore -- defaults to none
	ExcludeFirmwares     []string // denylist of firmwares to ignore -- defaults to none
	Distros              []string // allowlist of distributions to run test against -- defaults to all
	ExcludeDistros       []string // denylist of distributions to ignore -- defaults to none
	Architectures        []string // allowlist of machine architectures supported -- defaults to all
	ExcludeArchitectures []string // denylist of architectures to ignore -- defaults to none
	Flags                []Flag   // special-case options for this test
	Tags                 []string // list of tags that can be matched against -- defaults to none

	// Sizes of additional empty disks to attach to the node (e.g. ["1G",
	// "5G"]) -- defaults to none.
	AdditionalDisks []string

	// Minimum amount of memory required for test.
	MinMemory int

	// ExternalTest is a path to a binary that will be uploaded
	ExternalTest string
	// DependencyDir is a path to directory that will be uploaded, normally used by external tests
	DependencyDir string

	// FailFast skips any sub-test that occurs after a sub-test has
	// failed.
	FailFast bool

	// MinVersion prevents the test from executing on CoreOS machines
	// less than MinVersion. This will be ignored if the name fully
	// matches without globbing.
	MinVersion semver.Version

	// EndVersion prevents the test from executing on CoreOS machines
	// greater than or equal to EndVersion. This will be ignored if
	// the name fully matches without globbing.
	EndVersion semver.Version
}

// Registered tests that run as part of `kola run` live here. Mapping of names
// to tests.
var Tests = map[string]*Test{}

// Registered tests that run as part of `kola run-upgrade` live here. Mapping of
// names to tests.
var UpgradeTests = map[string]*Test{}

// Register is usually called via init() functions and is how kola test
// harnesses knows which tests it can choose from. Panics if existing name is
// registered
func Register(m map[string]*Test, t *Test) {
	_, ok := m[t.Name]
	if ok {
		panic(fmt.Sprintf("test %v already registered", t.Name))
	}

	if (t.EndVersion != semver.Version{}) && !t.MinVersion.LessThan(t.EndVersion) {
		panic(fmt.Sprintf("test %v has an invalid version range", t.Name))
	}

	m[t.Name] = t
}

func RegisterTest(t *Test) {
	Register(Tests, t)
}

func RegisterUpgradeTest(t *Test) {
	Register(UpgradeTests, t)
}

func (t *Test) HasFlag(flag Flag) bool {
	for _, f := range t.Flags {
		if f == flag {
			return true
		}
	}
	return false
}
