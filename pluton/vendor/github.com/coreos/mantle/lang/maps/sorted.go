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
	"reflect"
	"sort"

	"github.com/coreos/mantle/lang/natsort"
)

// Keys returns a map's keys as an unordered slice of strings.
func Keys(m interface{}) []string {
	mapValue := reflect.ValueOf(m)

	// Value.String() isn't sufficient to assert the keys are strings.
	if mapValue.Type().Key().Kind() != reflect.String {
		panic("maps: keys must be strings")
	}

	keyValues := mapValue.MapKeys()
	keys := make([]string, len(keyValues))
	for i, k := range keyValues {
		keys[i] = k.String()
	}

	return keys
}

// SortedKeys returns a map's keys as a sorted slice of strings.
func SortedKeys(m interface{}) []string {
	keys := Keys(m)
	sort.Strings(keys)
	return keys
}

// NaturalKeys returns a map's keys as a natural sorted slice of strings.
// See github.com/coreos/mantle/lang/natsort
func NaturalKeys(m interface{}) []string {
	keys := Keys(m)
	natsort.Strings(keys)
	return keys
}
