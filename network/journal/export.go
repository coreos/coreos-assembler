// Copyright 2017 CoreOS, Inc.
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

package journal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type ExportReader struct {
	buf *bufio.Reader
}

func NewExportReader(r io.Reader) *ExportReader {
	return &ExportReader{
		buf: bufio.NewReader(r),
	}
}

// ReadEntry reads one journal entry from the stream and returns it as a map.
func (e *ExportReader) ReadEntry() (Entry, error) {
	entry := make(Entry)
	for {
		name, value, err := e.readField()
		if err != nil {
			return nil, err
		}
		if name == "" {
			if len(entry) != 0 {
				// terminate entry on a trailing newline.
				return entry, nil
			}
			// skip any leading newlines.
			continue
		}
		entry[name] = value
	}
}

// read a text or binary field name and value.
func (e *ExportReader) readField() (name string, value []byte, err error) {
	line, err := e.readLine()
	if err != nil {
		return
	}
	if len(line) == 0 {
		return
	}

	eq := bytes.IndexByte(line, '=')
	if eq == 0 {
		err = errors.New("journal: empty field name")
		return
	} else if eq > 0 {
		name = string(line[:eq])
		value = line[eq+1:]
		return
	} else {
		name = string(line)
		value, err = e.readBinary()
		return
	}
}

// read the next line, trim the trailing newline.
func (e *ExportReader) readLine() ([]byte, error) {
	line, err := e.buf.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	// trim the trailing newline
	return line[:len(line)-1], nil
}

// read binary field value
func (e *ExportReader) readBinary() ([]byte, error) {
	// first, a little-endian 64bit data size
	size := make([]byte, 8)
	if _, err := io.ReadFull(e.buf, size); err != nil {
		return nil, err
	}

	// then, the data
	value := make([]byte, binary.LittleEndian.Uint64(size))
	if _, err := io.ReadFull(e.buf, value); err != nil {
		return nil, err
	}

	// finally, a trailing newline before the next field.
	if newline, err := e.buf.ReadByte(); err != nil {
		return nil, err
	} else if newline != '\n' {
		return nil, errors.New("journal: binary field missing terminating newline")
	}

	return value, nil
}
