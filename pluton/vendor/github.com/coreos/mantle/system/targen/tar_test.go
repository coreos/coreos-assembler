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
	"bytes"
	"io"
	"testing"

	"github.com/coreos/pkg/capnslog"
)

func TestTarGenBinary(t *testing.T) {
	if testing.Verbose() {
		capnslog.SetGlobalLogLevel(capnslog.TRACE)
	}

	bins := []string{"/bin/sh", "/bin/ls"}
	buf := new(bytes.Buffer)

	tg := New()

	for _, bin := range bins {
		tg.AddBinary(bin)
	}

	err := tg.Generate(buf)
	if err != nil {
		t.Fatal(err)
	}

	r := bytes.NewReader(buf.Bytes())
	tr := tar.NewReader(r)

	tarfiles := make(map[string]struct{})

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		t.Logf("tar file %q", hdr.Name)

		// check dups
		if _, ok := tarfiles[hdr.Name]; ok {
			t.Fatalf("found duplicate file %q", hdr.Name)
		}

		tarfiles[hdr.Name] = struct{}{}
	}

	for _, bin := range bins {
		libs, err := ldd(bin)
		if err != nil {
			t.Fatal(err)
		}

		libs = append(libs, bin)
		for _, file := range libs {
			if _, ok := tarfiles[file]; !ok {
				t.Fatalf("file %q is missing from the tarball", file)
			}
		}
	}
}
