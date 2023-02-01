// Copyright (c) 2016 VMware, Inc. All Rights Reserved.
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

package esx

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/vmware/govmomi/ovf"
)

type archive struct {
	path string
}

type archiveEntry struct {
	io.Reader
	f *os.File
}

func (t *archiveEntry) Close() error {
	return t.f.Close()
}

func (t *archive) readOvf(fpath string) ([]byte, error) {
	r, _, err := t.open(fpath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

func (t *archive) readEnvelope(fpath string) (*ovf.Envelope, error) {
	if fpath == "" {
		return nil, nil
	}

	r, _, err := t.open(fpath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	e, err := ovf.Unmarshal(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ovf: %s", err.Error())
	}

	return e, nil
}

func (t *archive) open(pattern string) (io.ReadCloser, int64, error) {
	f, err := os.Open(t.path)
	if err != nil {
		return nil, 0, err
	}

	r := tar.NewReader(f)

	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return nil, 0, err
		}

		matched, err := path.Match(pattern, path.Base(h.Name))
		if err != nil {
			f.Close()
			return nil, 0, err
		}

		if matched {
			return &archiveEntry{r, f}, h.Size, nil
		}
	}

	f.Close()
	return nil, 0, fmt.Errorf("couldn't find file in archive")
}
