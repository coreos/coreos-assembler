/*
   Copyright 2015 CoreOS, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package util

import (
	"os"
)

/*
#define _GNU_SOURCE
#include <fcntl.h>

enum {
	oTmpFile = O_TMPFILE,
};
*/
import "C"

// O_TMPFILE is missing from both os and sys/unix :(
// https://github.com/golang/go/issues/7830
const (
	O_TMPFILE = C.oTmpFile
)

// Copy the base image to a new temporary file.
func TempFile(dir string) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}

	// TODO: make O_EXCL optional to support debugging failures.
	return os.OpenFile(dir, O_TMPFILE|os.O_EXCL|os.O_RDWR, 0600)
}
