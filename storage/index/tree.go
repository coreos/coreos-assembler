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
	bucket  *storage.Bucket
	objects map[string][]*gs.Object
}

func NewIndexTree(bucket *storage.Bucket) *IndexTree {
	t := &IndexTree{
		bucket:  bucket,
		objects: make(map[string][]*gs.Object),
	}

	for _, obj := range FilteredObjects(bucket) {
		i := strings.LastIndexByte(obj.Name, '/')
		dir := obj.Name[:i+1]
		t.objects[dir] = append(t.objects[dir], obj)
	}

	return t
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
	for prefix := range t.objects {
		if strings.HasPrefix(prefix, dir) {
			name := prefix[len(dir):]
			if name == "" {
				continue
			}
			if i := strings.IndexByte(name, '/'); i >= 0 {
				name = name[:i]
				prefix = prefix[:len(dir)+i+1]
			}
			subdirs[name] = prefix
		}
	}
	return subdirs
}

func FilteredObjects(bucket *storage.Bucket) []*gs.Object {
	indexes := make(map[string]struct{})
	for _, prefix := range bucket.Prefixes() {
		indexes[prefix] = struct{}{}
		indexes[strings.TrimSuffix(prefix, "/")] = struct{}{}
		indexes[prefix+"index.html"] = struct{}{}
	}

	allobjs := bucket.Objects()
	filtered := make([]*gs.Object, 0, len(allobjs))
	for _, obj := range allobjs {
		if _, ok := indexes[obj.Name]; !ok {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}
