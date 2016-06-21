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

package generator

import (
	"bytes"
	"compress/bzip2"
	"io/ioutil"
	"testing"
)

func bunzip2(t *testing.T, z []byte) []byte {
	b, err := ioutil.ReadAll(bzip2.NewReader(bytes.NewReader(z)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBzip2(t *testing.T) {
	smallOnes, err := Bzip2(testOnes)
	if err != nil {
		t.Fatal(err)
	}

	bigOnes := bunzip2(t, smallOnes)
	if !bytes.Equal(bigOnes, testOnes) {
		t.Fatal("bzip2 corrupted the data")
	}
}
