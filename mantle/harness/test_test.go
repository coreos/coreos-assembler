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

package harness

import (
	"reflect"
	"testing"
)

func TestTestsAdd(t *testing.T) {
	var ts Tests

	ht1 := &HarnessTest{run: nil, timeout: 0}
	ht2 := &HarnessTest{run: nil, timeout: 2}
	ts.Add("test1", nil, 0)
	ts.Add("test2", nil, 2)
	expect := Tests(map[string]*HarnessTest{"test1": ht1, "test2": ht2})
	if !reflect.DeepEqual(ts, expect) {
		t.Errorf("got %v wanted %v", ts, expect)
	}
}

func TestTestsAddDup(t *testing.T) {
	var ts Tests
	ts.Add("test1", nil, 0)
	defer func() {
		if recover() == nil {
			t.Errorf("Add did not panic")
		}
	}()
	ts.Add("test1", nil, 0)
}

func TestTestsList(t *testing.T) {
	ts := Tests(map[string]*HarnessTest{"test01": nil, "test2": nil})
	list := ts.List()
	expect := []string{"test01", "test2"}
	if !reflect.DeepEqual(list, expect) {
		t.Errorf("got %v wanted %v", list, expect)
	}
}
