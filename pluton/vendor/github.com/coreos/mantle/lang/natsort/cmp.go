// Copyright 2016 CoreOS, Inc.
// Copyright 2000 Martin Pool <mbp@humbug.org.au>
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

// natsort implements Martin Pool's Natural Order String Comparison
// Original implementation: https://github.com/sourcefrog/natsort
//
// Strings are sorted as usual, except that decimal integer substrings
// are compared on their numeric value. For example:
//
//     a < a0 < a1 < a1a < a1b < a2 < a10 < a20
//
// All white space and control characters are ignored.
//
// Leading zeros are *not* ignored, which tends to give more
// reasonable results on decimal fractions:
//
//     1.001 < 1.002 < 1.010 < 1.02 < 1.1 < 1.3
//
package natsort

func isDigit(s string, i int) bool {
	return i < len(s) && s[i] >= '0' && s[i] <= '9'
}

// Compare unpadded numbers, such as a plain ol' integer.
func cmpInteger(a, b string, ai, bi *int) int {
	// The longest run of digits wins. That aside, the greatest
	// value wins, but we can't know that it will until we've
	// scanned both numbers to know that they have the same
	// magnitude, so we remember it in bias.
	var bias int
	for {
		aIsDigit := isDigit(a, *ai)
		bIsDigit := isDigit(b, *bi)
		switch {
		case !aIsDigit && !bIsDigit:
			return bias
		case !aIsDigit:
			return -1
		case !bIsDigit:
			return +1
		case bias == 0 && a[*ai] < b[*bi]:
			bias = -1
		case bias == 0 && a[*ai] > b[*bi]:
			bias = +1
		}
		*ai++
		*bi++
	}
}

// Compare zero padded numbers, such as the fractional part of a decimal.
func cmpFraction(a, b string, ai, bi *int) int {
	for {
		aIsDigit := isDigit(a, *ai)
		bIsDigit := isDigit(b, *bi)
		switch {
		case !aIsDigit && !bIsDigit:
			return 0
		case !aIsDigit:
			return -1
		case !bIsDigit:
			return +1
		case a[*ai] < b[*bi]:
			return -1
		case a[*ai] > b[*bi]:
			return +1
		}
		*ai++
		*bi++
	}
}

// Compare tests if a is less than, equal to, or greater than b according
// to the natural sorting algorithm, returning -1, 0, or +1 respectively.
func Compare(a, b string) int {
	var ai, bi int
	for {
		// Skip ASCII space and control characters. Keeping it simple
		// for the sake of speed. Checking specific chars and UTF-8
		// more than doubles the runtime for non-numeric strings.
		for ai < len(a) && a[ai] <= 32 {
			ai++
		}
		for bi < len(b) && b[bi] <= 32 {
			bi++
		}

		if isDigit(a, ai) && isDigit(b, bi) {
			if a[ai] == '0' || b[bi] == '0' {
				if r := cmpFraction(a, b, &ai, &bi); r != 0 {
					return r
				}
			} else {
				if r := cmpInteger(a, b, &ai, &bi); r != 0 {
					return r
				}
			}
		}

		switch {
		case ai >= len(a) && bi >= len(b):
			return 0
		case ai >= len(a):
			return -1
		case bi >= len(b):
			return +1
		case a[ai] < b[bi]:
			return -1
		case a[ai] > b[bi]:
			return +1
		}

		ai++
		bi++
	}
}
