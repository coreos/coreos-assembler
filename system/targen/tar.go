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

// exec is extension of the standard os.exec package.
// Adds a handy dandy interface and assorted other features.

package targen

import (
	"archive/tar"
	"io"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "targen")

type TarGen struct {
	files    []string
	binaries []string
}

func New() *TarGen {
	return &TarGen{}
}

func (t *TarGen) AddFile(path string) *TarGen {
	plog.Tracef("adding file %q", path)
	t.files = append(t.files, path)
	return t
}

func (t *TarGen) AddBinary(path string) *TarGen {
	plog.Tracef("adding binary %q", path)
	t.binaries = append(t.binaries, path)
	return t
}

func tarWriteFile(tw *tar.Writer, file string) error {
	plog.Tracef("writing file %q", file)

	st, err := os.Stat(file)
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    file,
		Size:    st.Size(),
		Mode:    int64(st.Mode()),
		ModTime: st.ModTime(),
	}

	if err = tw.WriteHeader(hdr); err != nil {
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}

	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return err
	}

	return nil
}

func (t *TarGen) Generate(w io.Writer) error {
	tw := tar.NewWriter(w)

	// store processed files here so we skip duplicates.
	copied := make(map[string]struct{})

	for _, file := range t.files {
		if _, ok := copied[file]; ok {
			plog.Tracef("skipping duplicate file %q", file)
			continue
		}

		plog.Tracef("copying file %q", file)

		if err := tarWriteFile(tw, file); err != nil {
			return err
		}

		copied[file] = struct{}{}
	}

	for _, binary := range t.binaries {
		libs, err := ldd(binary)
		if err != nil {
			return err
		}

		for _, lib := range libs {
			if _, ok := copied[lib]; ok {
				plog.Tracef("skipping duplicate library %q", lib)

				continue
			}

			plog.Tracef("copying library %q", lib)

			if err := tarWriteFile(tw, lib); err != nil {
				return err
			}

			copied[lib] = struct{}{}
		}

		if _, ok := copied[binary]; ok {
			plog.Tracef("skipping duplicate binary %q", binary)
			continue
		}

		plog.Tracef("copying binary %q", binary)

		if err := tarWriteFile(tw, binary); err != nil {
			return err
		}

		copied[binary] = struct{}{}
	}

	if err := tw.Close(); err != nil {
		return err
	}

	return nil
}
