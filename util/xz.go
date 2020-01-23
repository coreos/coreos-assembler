// Copyright 2018 Red Hat.
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

package util

import (
	"io"
	"os"
	"os/exec"

	"github.com/ulikunitz/xz"
)

func XzDecompressStream(out io.Writer, in io.Reader) error {
	// opportunistically use the `xz` CLI if available since it's way faster
	xzPath, err := exec.LookPath("xz")
	if err == nil {
		cmd := exec.Command(xzPath, "--decompress", "--stdout")
		cmd.Stdin = in
		cmd.Stdout = out
		return cmd.Run()
	}

	reader, err := xz.NewReader(in)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, reader)
	return err
}

// XzDecompressFile does xz decompression from src file into dst file
func XzDecompressFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	if err = XzDecompressStream(out, in); err != nil {
		os.Remove(dst)
	}
	return err
}
