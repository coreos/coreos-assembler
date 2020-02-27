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

package generator

import (
	"bytes"
	"io"
	"os"
	"os/exec"
)

type bzip2Writer struct {
	cmd *exec.Cmd
	in  io.WriteCloser
}

// NewBzip2Writer wraps a writer, compressing all data written to it.
func NewBzip2Writer(w io.Writer) (io.WriteCloser, error) {
	zipper, err := exec.LookPath("lbzip2")
	if err != nil {
		zipper = "bzip2"
	}

	cmd := exec.Command(zipper, "-c")
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	return &bzip2Writer{cmd, in}, cmd.Start()
}

func (bz *bzip2Writer) Write(p []byte) (n int, err error) {
	return bz.in.Write(p)
}

// Close stops the compressor, flushing out any remaining data.
// The underlying writer is not closed.
func (bz *bzip2Writer) Close() error {
	if err := bz.in.Close(); err != nil {
		return err
	}
	return bz.cmd.Wait()
}

// Bzip2 simplifies using a Bzip2Writer when working with in-memory buffers.
func Bzip2(data []byte) ([]byte, error) {
	buf := bytes.Buffer{}
	zip, err := NewBzip2Writer(&buf)
	if err != nil {
		return nil, err
	}

	if _, err := zip.Write(data); err != nil {
		return nil, err
	}

	if err := zip.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
