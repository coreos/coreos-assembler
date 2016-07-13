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

	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/storage"
)

type IndexSet map[string]struct{}

func NewIndexSet(bucket *storage.Bucket) IndexSet {
	is := IndexSet(make(map[string]struct{}))

	for _, prefix := range bucket.Prefixes() {
		is[prefix] = struct{}{}
		is[strings.TrimSuffix(prefix, "/")] = struct{}{}
		is[prefix+"index.html"] = struct{}{}
	}

	return is
}

func (is IndexSet) IsIndex(obj *gs.Object) bool {
	_, isIndex := is[obj.Name]
	return isIndex
}

func (is IndexSet) NotIndex(obj *gs.Object) bool {
	return !is.IsIndex(obj)
}
