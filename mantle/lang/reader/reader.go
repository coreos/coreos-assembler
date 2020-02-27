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
)

// AtReader converts an io.ReaderAt into an io.Reader
func AtReader(ra io.ReaderAt) io.Reader {
	if rd, ok := ra.(io.Reader); ok {
		return rd
	}
	return &atReader{ReaderAt: ra}
}

type atReader struct {
	io.ReaderAt
	off int64
}

func (r *atReader) Read(p []byte) (n int, err error) {
	n, err = r.ReadAt(p, r.off)
	r.off += int64(n)
	return
}
