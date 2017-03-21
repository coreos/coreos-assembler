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

package pluton

// A Test defines a function that will test a running Cluster. The test is run
// based on if its Name matches the glob pattern specified on the pluton
// command line.
type Test struct {
	Name    string
	Run     func(c *Cluster)
	Options Options
}

// Options represent per-test options that control the initial creation of a
// Cluster.  It is advised that all options are explicitly filled out for each
// registered test. TODO(pb): If this is too verbose to declare per test we may
// move to a default system but lose the nice struct declaration syntax.
// Eventually, we may allow overriding global options such as bootkube versions
// or cloud options.
type Options struct {
	SelfHostEtcd   bool
	InitialWorkers int
	InitialMasters int
}
