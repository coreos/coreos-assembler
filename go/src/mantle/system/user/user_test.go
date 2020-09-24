// Copyright 2015 CoreOS, Inc.
// Copyright 2011 The Go Authors.
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

package user

import (
	"fmt"
	"testing"
)

func TestCurrent(t *testing.T) {
	u, err := Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if u.HomeDir == "" {
		t.Errorf("didn't get a HomeDir")
	}
	if u.Username == "" {
		t.Errorf("didn't get a username")
	}
	if u.Groupname == "" {
		t.Errorf("didn't get a groupname")
	}
	if u.Uid != fmt.Sprintf("%d", u.UidNo) {
		t.Errorf("Uid %q and %d do not match", u.Uid, u.UidNo)
	}
	if u.Gid != fmt.Sprintf("%d", u.GidNo) {
		t.Errorf("Gid %q and %d do not match", u.Gid, u.GidNo)
	}
}

func compare(t *testing.T, want, got *User) {
	if want.Uid != got.Uid {
		t.Errorf("got Uid=%q; want %q", got.Uid, want.Uid)
	}
	if want.Username != got.Username {
		t.Errorf("got Username=%q; want %q", got.Username, want.Username)
	}
	if want.Name != got.Name {
		t.Errorf("got Name=%q; want %q", got.Name, want.Name)
	}
	if want.Gid != got.Gid {
		t.Errorf("got Gid=%q; want %q", got.Gid, want.Gid)
	}
	if want.Groupname != got.Groupname {
		t.Errorf("got Groupname=%q; want %q", got.Gid, want.Gid)
	}
	if want.HomeDir != got.HomeDir {
		t.Errorf("got HomeDir=%q; want %q", got.HomeDir, want.HomeDir)
	}
	if want.UidNo != got.UidNo {
		t.Errorf("got UidNo=%d; want %d", got.UidNo, want.UidNo)
	}
	if want.GidNo != got.GidNo {
		t.Errorf("got GidNo=%d; want %d", got.GidNo, want.GidNo)
	}
}

func TestLookup(t *testing.T) {
	want, err := Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	got, err := Lookup(want.Username)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	compare(t, want, got)
}

func TestLookupId(t *testing.T) {
	want, err := Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	got, err := LookupId(want.Uid)
	if err != nil {
		t.Fatalf("LookupId: %v", err)
	}
	compare(t, want, got)
}
