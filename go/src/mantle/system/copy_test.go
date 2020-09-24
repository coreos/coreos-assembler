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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func checkFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if info.Mode() != mode {
		t.Fatalf("Unexpected mode: %s != %s %s", info.Mode(), mode, path)
	}

	newData, err := ioutil.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, newData) {
		t.Fatalf("Unexpected data: %q != %q %s", data, string(newData), path)
	}
}

func TestCopyRegularFile(t *testing.T) {
	data := []byte("test")
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "src")
	if err := ioutil.WriteFile(src, data, 0600); err != nil {
		t.Fatal(err)
	}
	checkFile(t, src, data, 0600)

	copy1 := filepath.Join(tmp, "copy1")
	if err := CopyRegularFile(src, copy1); err != nil {
		t.Fatal(err)
	}
	checkFile(t, copy1, data, 0600)

	if err := os.Chmod(src, 0640); err != nil {
		t.Fatal(err)
	}
	copy2 := filepath.Join(tmp, "copy2")
	if err := CopyRegularFile(src, copy2); err != nil {
		t.Fatal(err)
	}
	checkFile(t, copy2, data, 0640)
}

func TestInstallRegularFile(t *testing.T) {
	data := []byte("test")
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "src")
	if err := ioutil.WriteFile(src, data, 0600); err != nil {
		t.Fatal(err)
	}
	checkFile(t, src, data, 0600)

	copy1 := filepath.Join(tmp, "subdir", "copy1")
	if err := InstallRegularFile(src, copy1); err != nil {
		t.Fatal(err)
	}
	checkFile(t, copy1, data, 0600)
}
