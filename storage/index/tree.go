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
	"strings"

	gs "github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/storage"
)

type IndexTree struct {
	bucket   *storage.Bucket
	prefixes map[string]bool
	objects  map[string][]*gs.Object
}

func NewIndexTree(bucket *storage.Bucket) *IndexTree {
	t := &IndexTree{
		bucket:   bucket,
		prefixes: make(map[string]bool),
		objects:  make(map[string][]*gs.Object),
	}

	for _, prefix := range bucket.Prefixes() {
		t.prefixes[prefix] = false // initialize as empty
	}

	indexes := NewIndexSet(bucket)
	for _, obj := range bucket.Objects() {
		if indexes.NotIndex(obj) {
			t.addObj(obj)
		}
	}

	return t
}

func (t *IndexTree) addObj(obj *gs.Object) {
	prefix := storage.NextPrefix(obj.Name)
	t.objects[prefix] = append(t.objects[prefix], obj)
	for !t.prefixes[prefix] {
		t.prefixes[prefix] = true // mark as not empty
		if prefix == "" {
			return
		}
		prefix = storage.NextPrefix(prefix)
	}
}

func (t *IndexTree) Objects(dir string) map[string]*gs.Object {
	files := make(map[string]*gs.Object)
	for _, obj := range t.objects[dir] {
		files[strings.TrimPrefix(obj.Name, dir)] = obj
	}
	return files
}

func (t *IndexTree) SubDirs(dir string) map[string]string {
	subdirs := make(map[string]string)
	for prefix, notEmpty := range t.prefixes {
		if notEmpty && strings.HasPrefix(prefix, dir) {
			name := strings.TrimSuffix(prefix[len(dir):], "/")
			if name == "" || strings.Contains(name, "/") {
				continue
			}
			subdirs[name] = prefix
		}
	}
	return subdirs
}

func (t *IndexTree) Prefixes(dir string) []string {
	prefixes := make([]string, 0, len(t.prefixes))
	for prefix := range t.prefixes {
		if strings.HasPrefix(prefix, dir) {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}
