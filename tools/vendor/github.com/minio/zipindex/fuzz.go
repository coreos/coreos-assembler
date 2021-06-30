// +build gofuzz

/*
 * zipindex, (C)2021 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package zipindex

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
)

// SET GO111MODULE=off&&go-fuzz-build -o=fuzz-build.zip&&go-fuzz -minimize=5s -timeout=60 -bin=fuzz-build.zip -workdir=fuzz

// Fuzz a roundtrip.
func Fuzz(b []byte) int {
	exitOnErr := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	sz := 1 << 10
	if sz > len(b) {
		sz = len(b)
	}
	var files Files
	var err error
	for {
		files, err = ReadDir(b[len(b)-sz:], int64(len(b)), nil)
		if err == nil {
			break
		}
		var terr ErrNeedMoreData
		if errors.As(err, &terr) {
			if terr.FromEnd > int64(len(b)) {
				return 0
			}
			sz = int(terr.FromEnd)
		} else {
			// Unable to parse...
			return 0
		}
	}
	// Serialize files to binary.
	serialized, err := files.Serialize()
	exitOnErr(err)

	// Deserialize the content.
	files, err = DeserializeFiles(serialized)
	exitOnErr(err)

	if len(files) == 0 {
		return 0
	}
	for _, file := range files {
		// Create a reader with entire zip file...
		rs := bytes.NewReader(b)
		// Seek to the file offset.
		_, err = rs.Seek(file.Offset, io.SeekStart)
		if err != nil {
			continue
		}

		// Provide the forwarded reader..
		rc, err := file.Open(rs)
		if err != nil {
			continue
		}
		defer rc.Close()

		// Read the zip file content.
		ioutil.ReadAll(rc)
	}

	return 1
}
