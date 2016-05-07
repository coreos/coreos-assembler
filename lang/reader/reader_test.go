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

package reader

import (
	"io"
	"strings"
	"testing"
)

func TestAtReader(t *testing.T) {
	ra := strings.NewReader("this is a test")
	rd := atReader{ra, 0}

	buf := make([]byte, 4)
	_, err := rd.Read(buf)
	if err != nil {
		t.Error(err)
	}
	if string(buf) != "this" {
		t.Errorf("Unexpected: %q", string(buf))
	}

	_, err = rd.Read(buf)
	if err != nil {
		t.Error(err)
	}
	if string(buf) != " is " {
		t.Errorf("Unexpected: %q", string(buf))
	}

	r := AtReader(ra)
	switch typ := r.(type) {
	case *strings.Reader:
	default:
		t.Errorf("Unexpected type %T", typ)
	}

	var rn io.ReaderAt
	r = AtReader(rn)
	switch typ := r.(type) {
	case *atReader:
	default:
		t.Errorf("Unexpected type %T", typ)
	}
}
