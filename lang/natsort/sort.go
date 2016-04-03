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

// Strings natural sorts a slice of strings.
func Strings(s []string) {
	sort.Sort(StringSlice(s))
}

// StringsAreSorted tests whether a slice of strings is natural sorted.
func StringsAreSorted(s []string) bool {
	return sort.IsSorted(StringSlice(s))
}

// StringSlice provides sort.Interface for natural sorting []string.
type StringSlice []string

func (ss StringSlice) Len() int {
	return len(ss)
}

func (ss StringSlice) Less(i, j int) bool {
	if r := Compare(ss[i], ss[j]); r == 0 {
		// If the strings compare the same ignoring spaces try again
		// with a full comparison to help ensure stable sorts.
		return ss[i] < ss[j]
	} else {
		return r < 0
	}
}

func (ss StringSlice) Swap(i, j int) {
	ss[i], ss[j] = ss[j], ss[i]
}
