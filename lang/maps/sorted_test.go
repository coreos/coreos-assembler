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

package maps

import (
	"sort"
	"testing"
)

var (
	// some random goo
	testKeys = []string{
		"aicuquie",
		"loocheiv",
		"phiexieh",
		"cheuzash",
		"ohbohmop",
		"oobeecoh",
		"ohxadupu",
		"yuilohsh",
		"oongoojo",
		"mielutao",
		"iriecier",
		"eisheiba",
		"ahsoogup",
		"aabeevie",
		"aeyaebek",
		"kaibahgh",
	}
)

func TestSortedKeys(t *testing.T) {
	testMap := make(map[string]bool)
	for _, k := range testKeys {
		testMap[k] = true
	}

	// test is pointless if map iterates in-order by random chance
	mapKeys := make([]string, 0, len(testMap))
	for k, _ := range testMap {
		mapKeys = append(mapKeys, k)
	}
	if sort.StringsAreSorted(mapKeys) {
		t.Skip("map is already iterating in order!")
	}

	sortedKeys := SortedKeys(testMap)
	if !sort.StringsAreSorted(sortedKeys) {
		t.Error("SortedKeys did not sort the keys!")
	}

	if len(sortedKeys) != len(testKeys) {
		t.Errorf("SortedKeys returned %d keys, not %d",
			len(sortedKeys), len(testKeys))
	}
}
