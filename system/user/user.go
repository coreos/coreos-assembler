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

// user is extension of the standard os.user package which supports groups.
package user

import (
	"os/user"
)

// User represents a user account.
type User struct {
	*user.User
	Groupname string
	// For convenience so users don't need to strconv themselves.
	UidNo int
	GidNo int
}

// Current returns the current user.
func Current() (*User, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return lookupGroup(u)
}

// Lookup looks up a user by username.
func Lookup(username string) (*User, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	return lookupGroup(u)
}

// LookupId looks up a user by userid.
func LookupId(uid string) (*User, error) {
	u, err := user.LookupId(uid)
	if err != nil {
		return nil, err
	}
	return lookupGroup(u)
}
