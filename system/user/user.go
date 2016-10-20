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

// user provides an extended User struct to simplify usage
package user

import (
	"os/user"
	"strconv"
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
	return newUser(user.Current())
}

// Lookup looks up a user by username.
func Lookup(username string) (*User, error) {
	return newUser(user.Lookup(username))
}

// LookupId looks up a user by userid.
func LookupId(uid string) (*User, error) {
	return newUser(user.LookupId(uid))
}

// Convert the stock user.User to our own User strict with group info.
func newUser(u *user.User, err error) (*User, error) {
	if err != nil {
		return nil, err
	}

	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return nil, err
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, err
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}

	return &User{
		User:      u,
		Groupname: g.Name,
		UidNo:     uid,
		GidNo:     gid,
	}, nil
}
