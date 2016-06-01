// Copyright 2015 CoreOS, Inc.
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

package system

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/sys/unix"
)

type AnonFile struct {
	os.File
}

// Link the anonymous file into the filesystem. The caller should ensure the
// new path is on the same filesystem as the directory passed to AnonymousFile.
// This may be called multiple times and does not influence the output of Name.
func (a *AnonFile) Link(name string) error {
	// The AT_EMPTY_PATH version needs CAP_DAC_READ_SEARCH but using
	// /proc and AT_SYMLINK_FOLLOW does not and is the "normal" way.
	//Linkat(int(a.Fd()), "", AT_FDCWD, name, AT_EMPTY_PATH)
	err := unix.Linkat(
		unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", a.Fd()),
		unix.AT_FDCWD, name, unix.AT_SYMLINK_FOLLOW)
	if err != nil {
		return &os.LinkError{
			Op:  "linkat",
			Old: a.Name(),
			New: name,
			Err: err,
		}
	}
	return nil
}

// AnonymousFile creates an unlinked temporary file in the given directory
// or the default temporary directory if unspecified. Since the file has no
// name, the file's Name method does not return a real path.
func AnonymousFile(dir string) (*AnonFile, error) {
	if dir == "" {
		dir = os.TempDir()
	}

	anonPath := filepath.Join(dir, "(unlinked)")
	anonFd, err := unix.Open(
		dir, unix.O_RDWR|unix.O_TMPFILE|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, &os.PathError{
			Op:   "openat",
			Path: anonPath,
			Err:  err,
		}
	}

	return &AnonFile{*os.NewFile(uintptr(anonFd), anonPath)}, nil
}
