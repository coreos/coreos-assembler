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

package reporters

import (
	"time"

	"github.com/coreos/mantle/harness/testresult"
)

type Reporters []Reporter

func (reps Reporters) ReportTest(name string, subtests []string, result testresult.TestResult, duration time.Duration, b []byte) {
	for _, r := range reps {
		r.ReportTest(name, subtests, result, duration, b)
	}
}

func (reps Reporters) Output(path string) error {
	for _, r := range reps {
		err := r.Output(path)
		if err != nil {
			return err
		}
	}
	return nil
}

func (reps Reporters) SetResult(s testresult.TestResult) {
	for _, r := range reps {
		r.SetResult(s)
	}
}

type Reporter interface {
	ReportTest(string, []string, testresult.TestResult, time.Duration, []byte)
	Output(string) error
	SetResult(testresult.TestResult)
}
