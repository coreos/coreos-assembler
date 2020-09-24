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

package exec

import (
	"fmt"
	"os"
	"testing"
)

var testMulticall Entrypoint

func init() {
	testMulticall = NewEntrypoint("testMulticall", testMulticallHelper)
}

func testMulticallHelper(args []string) error {
	fmt.Println(args)
	return nil
}

func TestMain(m *testing.M) {
	MaybeExec()
	os.Exit(m.Run())
}

func TestMulticallNoArgs(t *testing.T) {
	cmd := testMulticall.Command()
	out, err := cmd.Output()
	if err != nil {
		t.Error(err)
	}
	if string(out) != "[]\n" {
		t.Errorf("Unexpected output: %q", string(out))
	}
}

func TestMulticallWithArgs(t *testing.T) {
	cmd := testMulticall.Command("arg1", "020")
	out, err := cmd.Output()
	if err != nil {
		t.Error(err)
	}
	if string(out) != "[arg1 020]\n" {
		t.Errorf("Unexpected output: %q", string(out))
	}
}
