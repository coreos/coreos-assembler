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

package natsort

import (
	"sort"
)

// Less determines if a naturally comes before b. Unlike Compare it will
// fall back to a normal string comparison which considers spaces if the
// two would otherwise be equal. That helps ensure the stability of sorts.
func Less(a, b string) bool {
	if r := Compare(a, b); r == 0 {
		return a < b
	} else {
		return r < 0
	}
}

// Strings natural sorts a slice of strings.
func Strings(s []string) {
	sort.Slice(s, func(i, j int) bool {
		return Less(s[i], s[j])
	})
}

// StringsAreSorted tests whether a slice of strings is natural sorted.
func StringsAreSorted(s []string) bool {
	return sort.SliceIsSorted(s, func(i, j int) bool {
		return Less(s[i], s[j])
	})
}
