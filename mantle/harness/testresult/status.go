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

package testresult

import "os"

const (
	Fail TestResult = "FAIL"
	Warn TestResult = "WARN"
	Skip TestResult = "SKIP"
	Pass TestResult = "PASS"
)

type TestResult string

func (s TestResult) Display() string {
	if term, has_term := os.LookupEnv("TERM"); !has_term || term == "" {
		return string(s)
	}

	red := "\033[31m"
	yellow := "\033[33m"
	blue := "\033[34m"
	green := "\033[32m"
	reset := "\033[0m"

	if s == Fail {
		return red + string(s) + reset
	} else if s == Warn {
		return yellow + string(s) + reset
	} else if s == Skip {
		return blue + string(s) + reset
	} else {
		return green + string(s) + reset
	}
}
