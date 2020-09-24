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
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/coreos/mantle/harness/testresult"
)

type jsonReporter struct {
	Tests    []jsonTest            `json:"tests"`
	Result   testresult.TestResult `json:"result"`
	filename string

	// Context variables
	Platform string `json:"platform"`
	Version  string `json:"version"`
}

type jsonTest struct {
	Name     string                `json:"name"`
	Result   testresult.TestResult `json:"result"`
	Duration time.Duration         `json:"duration"`
	Output   string                `json:"output"`
}

func NewJSONReporter(filename, platform, version string) *jsonReporter {
	return &jsonReporter{
		Platform: platform,
		Version:  version,
		filename: filename,
	}
}

func (r *jsonReporter) ReportTest(name string, result testresult.TestResult, duration time.Duration, b []byte) {
	r.Tests = append(r.Tests, jsonTest{
		Name:     name,
		Result:   result,
		Duration: duration,
		Output:   string(b),
	})
}

func (r *jsonReporter) Output(path string) error {
	f, err := os.Create(filepath.Join(path, r.filename))
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(r)
}

func (r *jsonReporter) SetResult(result testresult.TestResult) {
	r.Result = result
}
