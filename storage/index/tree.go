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
	"path"
	"strings"

	gs "github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/storage"
)

type IndexTree struct {
	bucket   *storage.Bucket
	indexes  map[string]*gs.Object
	objcount map[string]uint
	objects  map[string][]*gs.Object
}

func NewIndexTree(bucket *storage.Bucket) *IndexTree {
	t := &IndexTree{
		bucket:   bucket,
		indexes:  make(map[string]*gs.Object),
		objcount: make(map[string]uint),
		objects:  make(map[string][]*gs.Object),
	}

	for _, prefix := range bucket.Prefixes() {
		t.addDir(prefix)
	}

	for _, obj := range bucket.Objects() {
		if t.IsIndex(obj) {
			t.indexes[obj.Name] = obj
		} else {
			t.addObj(obj)
		}
	}

	return t
}

func dirIndexes(dir string) []string {
	indexes := []string{dir + "index.html"}
	if dir != "" {
		indexes = append(indexes, dir, strings.TrimSuffix(dir, "/"))
	}
	return indexes
}

func (t *IndexTree) addDir(dir string) {
	for _, index := range dirIndexes(dir) {
		t.indexes[index] = nil
	}
	t.objcount[dir] = 0
}

func nextPrefix(name string) string {
	prefix, _ := path.Split(strings.TrimSuffix(name, "/"))
	return prefix
}

func (t *IndexTree) addObj(obj *gs.Object) {
	prefix := nextPrefix(obj.Name)
	t.objects[prefix] = append(t.objects[prefix], obj)
	for {
		t.objcount[prefix]++
		if prefix == "" {
			return
		}
		prefix = nextPrefix(prefix)
	}
}

func (t *IndexTree) IsIndex(obj *gs.Object) bool {
	_, isIndex := t.indexes[obj.Name]
	return isIndex
}

func (t *IndexTree) IsNotIndex(obj *gs.Object) bool {
	return !t.IsIndex(obj)
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
	for prefix, sum := range t.objcount {
		if sum > 0 && strings.HasPrefix(prefix, dir) {
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
	prefixes := make([]string, 0, len(t.objcount))
	for prefix, _ := range t.objcount {
		if strings.HasPrefix(prefix, dir) {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}
