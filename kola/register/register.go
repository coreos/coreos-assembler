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
	NoEnableSelinux                    // don't enable selinux when starting or rebooting a machine
	RequiresInternetAccess             // run the test only if the platform supports Internet access
)

var (
	// platforms that have no Internet access
	PlatformsNoInternet = []string{
		"qemu",
	}
)

// Wrapper for the NativeFunc which includes an optional string of arches to exclude for each native test
type NativeFuncWrap struct {
	NativeFunc           func() error
	ExcludeArchitectures []string
}

// Simple constructor for returning NativeFuncWrap structure
func CreateNativeFuncWrap(f func() error, excludearches ...string) NativeFuncWrap {
	return NativeFuncWrap{f, excludearches}
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
	UserDataV3           *conf.UserData
	ClusterSize          int
	Platforms            []string // whitelist of platforms to run test against -- defaults to all
	ExcludePlatforms     []string // blacklist of platforms to ignore -- defaults to none
	Distros              []string // whitelist of distributions to run test against -- defaults to all
	ExcludeDistros       []string // blacklist of distributions to ignore -- defaults to none
	Architectures        []string // whitelist of machine architectures supported -- defaults to all
	ExcludeArchitectures []string // blacklist of architectures to ignore -- defaults to none
	Flags                []Flag   // special-case options for this test

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

func (t *Test) HasFlag(flag Flag) bool {
	for _, f := range t.Flags {
		if f == flag {
			return true
		}
	}
	return false
}
