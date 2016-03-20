// Copyright 2016 CoreOS, Inc.
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

package index

import (
	"fmt"
	"testing"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"
)

func TestBuildRedirect(t *testing.T) {
	root, err := NewDirectory("gs://bucket/")
	if err != nil {
		t.Fatal(err)
	}

	if err := root.AddObject(&storage.Object{Name: "foo/obj"}); err != nil {
		t.Fatal(err)
	}

	r, _ := buildRedirect(root.SubDirs["foo"])
	if r.Size == 0 || r.Crc32c == "" {
		t.Error("redirect is empty")
	}
	fmt.Println(r.Size)
}

func BenchmarkBuildRedirect(b *testing.B) {
	root, err := NewDirectory("gs://bucket/")
	if err != nil {
		b.Fatal(err)
	}

	if err := root.AddObject(&storage.Object{Name: "foo/obj"}); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildRedirect(root.SubDirs["foo"])
	}
}
