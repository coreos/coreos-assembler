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
	"math/rand"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

const (
	testDates = `2000-1-10
2000-1-2
1999-12-25
2000-3-23
1999-3-3`
	sortedDates = `1999-3-3
1999-12-25
2000-1-2
2000-1-10
2000-3-23`
	testFractions = `Fractional release numbers
1.011.02
1.010.12
1.009.02
1.009.20
1.009.10
1.002.08
1.002.03
1.002.01`
	sortedFractions = `1.002.01
1.002.03
1.002.08
1.009.02
1.009.10
1.009.20
1.010.12
1.011.02
Fractional release numbers`
	testWords = `fred
pic2
pic100a
pic120
pic121
jane
tom
pic02a
pic3
pic4
1-20
pic100
pic02000
10-20
1-02
1-2
x2-y7
x8-y8
x2-y08
x2-g8
pic01
pic02
pic 6
pic   7
pic 5
pic05
pic 5 
pic 5 something
pic 4 else`
	sortedWords = `1-02
1-2
1-20
10-20
fred
jane
pic01
pic02
pic02a
pic02000
pic05
pic2
pic3
pic4
pic 4 else
pic 5
pic 5 
pic 5 something
pic 6
pic   7
pic100
pic100a
pic120
pic121
tom
x2-g8
x2-y08
x2-y7
x8-y8`
	testEq = `a     a
a  a
aa
a   a
a a
a    a`
	sortedEq = `a     a
a    a
a   a
a  a
a a
aa`
)

func randomize(ss []string) {
	for i := range ss {
		j := rand.Intn(i + 1)
		ss[i], ss[j] = ss[j], ss[i]
	}
}

func doTestSlice(t *testing.T, testSlice, sortedSlice []string) {
	Strings(testSlice)
	if diff := pretty.Compare(sortedSlice, testSlice); diff != "" {
		t.Errorf("Unexpected order: %s", diff)
	}
}

func doTest(t *testing.T, testData, sortedData string) {
	testSlice := strings.Split(testData, "\n")
	sortedSlice := strings.Split(sortedData, "\n")
	if !StringsAreSorted(sortedSlice) {
		t.Errorf("StringsAreSorted claims unsorted: %#v", sortedSlice)
	}
	doTestSlice(t, testSlice, sortedSlice)
	randomize(testSlice)
	doTestSlice(t, testSlice, sortedSlice)
}

func TestDates(t *testing.T) {
	doTest(t, testDates, sortedDates)
}

func TestFractions(t *testing.T) {
	doTest(t, testFractions, sortedFractions)
}

func TestWords(t *testing.T) {
	doTest(t, testWords, sortedWords)
}

func TestEqual(t *testing.T) {
	doTest(t, testEq, sortedEq)
}
