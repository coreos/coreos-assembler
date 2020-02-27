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
	if IsOpNotSupported(err) {
		t.Skip("O_TMPFILE not supported")
	} else if err != nil {
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
	if err := LinkFile(anon, name); err != nil {
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

func TestLinkFile(t *testing.T) {
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	orig, err := ioutil.TempFile(tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer orig.Close()

	info, err := ioutil.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 1 || info[0].Name() != filepath.Base(orig.Name()) {
		t.Fatalf("%s has unexpected contents: %v", tmp, info)
	}

	// LinkFile while orig still exists should work
	if err := LinkFile(orig, filepath.Join(tmp, "name1")); err != nil {
		t.Errorf("Link failed: %v", err)
	}

	if err := os.Remove(orig.Name()); err != nil {
		t.Fatal(err)
	}

	// name1 is keeping orig alive so this still works
	if err := LinkFile(orig, filepath.Join(tmp, "name2")); err != nil {
		t.Errorf("Link failed: %v", err)
	}

	if err := os.Remove(filepath.Join(tmp, "name1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tmp, "name2")); err != nil {
		t.Fatal(err)
	}

	// LinkFile after orig is removed doesn't work which is a
	// difference between how normal files and O_TMPFILE works.
	if err := LinkFile(orig, filepath.Join(tmp, "name3")); err == nil {
		t.Error("Linking to removed file unexpectedly worked!")
	}
}

func TestPrivateFile(t *testing.T) {
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	priv, err := PrivateFile(tmp)
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
	defer priv.Close()

	info, err := ioutil.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 0 {
		t.Errorf("%s not empty: %v", tmp, info)
	}

	if err := LinkFile(priv, filepath.Join(tmp, "name")); err == nil {
		t.Error("Linking to private file unexpectedly worked!")
	}
}
