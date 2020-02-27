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

package sdk

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

var versionTestData = []struct {
	Versions
	Text string
}{{
	Text: `
OREOS_BUILD=968
COREOS_BRANCH=2
COREOS_PATCH=0
COREOS_VERSION=968.2.0
COREOS_VERSION_ID=968.2.0
COREOS_BUILD_ID=""
COREOS_SDK_VERSION=967.0.0
`,
	Versions: Versions{
		Version:    "968.2.0",
		VersionID:  "968.2.0",
		BuildID:    "",
		SDKVersion: "967.0.0",
	}}, {
	Text: `
COREOS_VERSION='968.2.0+build-foo'
COREOS_VERSION_ID=968.2.0
COREOS_BUILD_ID="build-foo"
COREOS_SDK_VERSION=967.0.0
`,
	Versions: Versions{
		Version:    "968.2.0+build-foo",
		VersionID:  "968.2.0",
		BuildID:    "build-foo",
		SDKVersion: "967.0.0",
	}}}

func TestVersionsFromDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	filename := filepath.Join(dir, "version.txt")
	for _, data := range versionTestData {
		err = ioutil.WriteFile(filename, []byte(data.Text), 0600)
		if err != nil {
			t.Fatal(err)
		}
		ver, err := VersionsFromDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if diff := pretty.Compare(data.Versions, ver); diff != "" {
			t.Error(diff)
		}
	}
}
