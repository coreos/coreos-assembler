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
	"strings"
	"testing"
)

func testCompare(t *testing.T, a, b string) {
	if result := Compare(a, b); result != -1 {
		t.Errorf("Compare(%q, %q) = %+d, expected -1", a, b, result)
	}
	if result := Compare(b, a); result != +1 {
		t.Errorf("Compare(%q, %q) = %+d, expected +1", a, b, result)
	}
}

func testCompareEq(t *testing.T, a, b string) {
	if result := Compare(a, b); result != 0 {
		t.Errorf("Compare(%q, %q) = %+d, expected 0", a, b, result)
	}
}

func testList(t *testing.T, l []string) {
	for i := 0; i < len(l)-1; i++ {
		testCompare(t, l[i], l[i+1])
	}
}

func TestCompare01(t *testing.T) {
	testCompare(t, "01", "02")
}

func TestCompare02(t *testing.T) {
	testCompare(t, "02", "2")
}

func TestCompare10(t *testing.T) {
	testCompare(t, "2", "10")
}

func TestCompare100a(t *testing.T) {
	testCompare(t, "100a", "120")
}

func TestCompare001a(t *testing.T) {
	testCompare(t, "001a", "0012")
}

func TestCompareSpace(t *testing.T) {
	testCompare(t, "a 1", "a2")
	testCompare(t, "a1", "a 2")
	testCompare(t, " 1", "2")
	testCompare(t, "1", " 2")
	testCompareEq(t, "a a", "aa")
}

func TestExample1(t *testing.T) {
	testList(t, strings.Split("a a0 a1 a1a a1b a2 a10 a20", " "))
}

func TestExample2(t *testing.T) {
	testList(t, strings.Split("1.001 1.002 1.010 1.02 1.1 1.3", " "))
}

func BenchmarkCompareFraction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Compare("0000005", "0000006")
	}
}

func BenchmarkCompareInteger(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Compare("5000000", "6000000")
	}
}

func BenchmarkCompareWords(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Compare("notnum.", "notnum!")
	}
}
