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
	"os/user"
	"strconv"
	"syscall"
)

/*
#include <grp.h>
#include <stdlib.h>
#include <sys/types.h>
#include <unistd.h>

// Go cannot figure out that gid_t == __gid_t so let C do it.
static int mygetgrgid_r(gid_t gid, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
	return getgrgid_r(gid, grp, buf, buflen, result);
}
*/
import "C"

// Based on Go's src/os/user/lookup_unix.go
func lookupGroup(u *user.User) (*User, error) {
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}

	var grp C.struct_group
	var result *C.struct_group
	buflen := C.sysconf(C._SC_GETGR_R_SIZE_MAX)
	if buflen <= 0 || buflen > 1<<20 {
		return nil, fmt.Errorf("unreasonable _SC_GETGR_R_SIZE_MAX of %d", buflen)
	}
	buf := C.malloc(C.size_t(buflen))
	defer C.free(buf)

	r := C.mygetgrgid_r(C.gid_t(gid), &grp,
		(*C.char)(buf),
		C.size_t(buflen),
		&result)
	if r != 0 {
		return nil, fmt.Errorf("lookup gid %d: %s", gid, syscall.Errno(r))
	}
	if result == nil {
		return nil, fmt.Errorf("lookup gid %d failed", gid)
	}

	return &User{
		User:      u,
		Groupname: C.GoString(grp.gr_name),
		UidNo:     uid,
		GidNo:     gid,
	}, nil
}
