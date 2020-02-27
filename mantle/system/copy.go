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
	"io"
	"os"
	"path/filepath"
)

// CopyRegularFile copies a file in place, updates are not atomic. If
// the destination doesn't exist it will be created with the same
// permissions as the original but umask is respected. If the
// destination already exists the permissions will remain as-is.
func CopyRegularFile(src, dest string) (err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	mode := srcInfo.Mode()
	if !mode.IsRegular() {
		return fmt.Errorf("Not a regular file: %s", src)
	}

	destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() {
		e := destFile.Close()
		if err == nil {
			err = e
		}
	}()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// InstallRegularFile copies a file, creating any parent directories.
func InstallRegularFile(src, dest string) error {
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	return CopyRegularFile(src, dest)
}
