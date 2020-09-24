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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/coreos/mantle/harness"
)

type X struct {
	*harness.H
	defaults map[string]string
}

func (x *X) Option(key string) string {
	env := "TEST_DATA_" + key
	if value := os.Getenv(env); value != "" {
		return value
	}

	if value, ok := x.defaults[key]; ok {
		return value
	}

	x.Skipf("Missing %q in environment.", env)
	return ""
}

type Test struct {
	Name     string
	Run      func(x *X)
	Defaults map[string]string
}

var tests harness.Tests

func RegisterTest(test Test) {
	// copy map to prevent surprises
	defaults := make(map[string]string)
	for k, v := range test.Defaults {
		defaults[k] = v
	}

	tests.Add(test.Name, func(h *harness.H) {
		test.Run(&X{H: h, defaults: defaults})
	})
}

func main() {
	opts := harness.Options{OutputDir: "_x_temp"}
	opts.FlagSet("", flag.ExitOnError).Parse(os.Args[1:])

	suite := harness.NewSuite(opts, tests)
	if err := suite.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Println("FAIL")
		os.Exit(1)
	}
	fmt.Println("PASS")
	os.Exit(0)
}
