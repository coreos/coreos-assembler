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
	"syscall"

	"golang.org/x/sys/unix"
)

// LinkFile creates a new link to an open File instead of an existing
// name as os.Link and friends do. Particularly useful for making a file
// created by AnonymousFile accessible in the filesystem. As with Link the
// caller should ensure the new name is on the same filesystem.
func LinkFile(file *os.File, name string) error {
	// The AT_EMPTY_PATH version needs CAP_DAC_READ_SEARCH but using
	// /proc and AT_SYMLINK_FOLLOW does not and is the "normal" way.
	//Linkat(int(a.Fd()), "", AT_FDCWD, name, AT_EMPTY_PATH)
	err := unix.Linkat(
		unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", file.Fd()),
		unix.AT_FDCWD, name, unix.AT_SYMLINK_FOLLOW)
	if err != nil {
		return &os.LinkError{
			Op:  "linkat",
			Old: file.Name(),
			New: name,
			Err: err,
		}
	}
	return nil
}

// AnonymousFile creates an unlinked temporary file in the given directory
// or the default temporary directory if unspecified. Since the file has no
// name, the file's Name method does not return a real path. The file may
// be later linked into the filesystem for safe keeping using LinkFile.
func AnonymousFile(dir string) (*os.File, error) {
	return tmpFile(dir, false)
}

// PrivateFile creates an unlinked temporary file in the given directory
// or the default temporary directory if unspecified. Unlike AnonymousFile,
// the opened file cannot be linked into the filesystem later.
func PrivateFile(dir string) (*os.File, error) {
	return tmpFile(dir, true)
}

func tmpFile(dir string, private bool) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}

	flags := unix.O_RDWR | unix.O_TMPFILE | unix.O_CLOEXEC
	if private {
		flags |= unix.O_EXCL
	}

	tmpPath := filepath.Join(dir, "(unlinked)")
	tmpFd, err := unix.Open(dir, flags, 0600)
	if err != nil {
		return nil, &os.PathError{
			Op:   "openat",
			Path: tmpPath,
			Err:  err,
		}
	}

	return os.NewFile(uintptr(tmpFd), tmpPath), nil
}

// IsOpNotSupported reports true if the underlying error was EOPNOTSUPP.
// Useful for checking if the host or filesystem lacks O_TMPFILE support.
func IsOpNotSupported(err error) bool {
	if oserr, ok := err.(*os.PathError); ok {
		if errno, ok := oserr.Err.(syscall.Errno); ok {
			if errno == syscall.EOPNOTSUPP {
				return true
			}
		}
	}
	return false
}
