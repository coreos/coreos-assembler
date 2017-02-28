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

package storage

import (
	"strings"
	"testing"

	"google.golang.org/api/storage/v1"
)

const (
	testPage = `<html><head>
<meta http-equiv="refresh" content="0;url=amd64-usr/">
</head></html>
`
	testPageCRC  = "xH9jaw=="
	testPageMD5  = "2a6rirkVBEsl0bzTOzNtzA=="
	testPageSize = 83
)

func TestSortObjects(t *testing.T) {
	slice := []*storage.Object{
		&storage.Object{Name: "a2"},
		&storage.Object{Name: "a10"},
		&storage.Object{Name: "a1"},
	}
	SortObjects(slice)
	if slice[0].Name != "a1" ||
		slice[1].Name != "a2" ||
		slice[2].Name != "a10" {
		t.Errorf("Undexpected order: %#v", slice)
	}
}

func TestCRCSum(t *testing.T) {
	obj := storage.Object{}
	if err := crcSum(&obj, strings.NewReader(testPage)); err != nil {
		t.Fatal(err)
	}
	if obj.Crc32c != testPageCRC {
		t.Errorf("Bad CRC32c: %q != %q", obj.Crc32c, testPageCRC)
	}
	if obj.Size != testPageSize {
		t.Errorf("Bad Size: %d != %d", obj.Size, testPageSize)
	}
}

func TestCRCEq(t *testing.T) {
	obj := storage.Object{Crc32c: testPageCRC, Size: testPageSize}
	if crcEq(&obj, nil) {
		t.Errorf("%#v equal to nil", obj)
	}
	if crcEq(nil, &obj) {
		t.Errorf("nil equal to %#v", obj)
	}
	if crcEq(nil, nil) {
		t.Error("nil not equal to nil")
	}
	if crcEq(&obj, &storage.Object{Crc32c: testPageCRC}) {
		t.Errorf("%#v equal ignored size", obj)
	}
	if crcEq(&obj, &storage.Object{Size: testPageSize}) {
		t.Errorf("%#v equal ignored blank CRC", obj)
	}
	if !crcEq(&obj, &obj) {
		t.Errorf("%#v not equal to itself", obj)
	}
}

func TestCRCSumAndEq(t *testing.T) {
	var a, b storage.Object
	r := strings.NewReader(testPage) // reading twice should work
	if err := crcSum(&a, r); err != nil {
		t.Fatal(err)
	}
	r.Seek(0, 0)
	if err := crcSum(&b, r); err != nil {
		t.Fatal(err)
	}
	if !crcEq(&a, &b) {
		t.Errorf("%#v not equal to %#v", a, b)
	}
	c := storage.Object{Crc32c: testPageCRC, Size: testPageSize}
	if !crcEq(&a, &c) {
		t.Errorf("%#v not equal to %#v", a, c)
	}
}
