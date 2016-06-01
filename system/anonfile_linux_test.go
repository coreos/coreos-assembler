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

package system

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAnonymousFile(t *testing.T) {
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	anon, err := AnonymousFile(tmp)
	if err != nil {
		// Travis is an unfun stick in the mud and gives us
		// an ancient system lacking O_TMPFILE support.
		if oserr, ok := err.(*os.PathError); ok {
			if errno, ok := oserr.Err.(syscall.Errno); ok {
				if errno == syscall.EOPNOTSUPP {
					t.Skip("O_TMPFILE not supported")
				}
			}
		}
		t.Fatal(err)
	}
	defer anon.Close()

	info, err := ioutil.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 0 {
		t.Errorf("%s not empty: %v", tmp, info)
	}

	name := filepath.Join(tmp, "name")
	if err := anon.Link(name); err != nil {
		t.Errorf("Link failed: %v", err)
	}

	info, err = ioutil.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 1 || info[0].Name() != "name" {
		t.Errorf("%s has unexpected contents: %v", tmp, info)
	}
}
