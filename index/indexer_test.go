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

package index

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"testing"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"
)

func TestBuildIndex(t *testing.T) {
	root, err := NewDirectory("gs://bucket/")
	if err != nil {
		t.Fatal(err)
	}

	if err := root.AddObject(&storage.Object{Name: "foo"}); err != nil {
		t.Fatal(err)
	}

	ix1, _, err := buildIndex(root, "index.html")
	if err != nil {
		t.Fatal(err)
	}

	if err := root.AddObject(&storage.Object{Name: "bar"}); err != nil {
		t.Fatal(err)
	}

	ix2, _, err := buildIndex(root, "index.html")
	if err != nil {
		t.Fatal(err)
	}

	if ix1.Size >= ix2.Size || ix1.Crc32c == ix2.Crc32c {
		t.Errorf("index didn't change after adding bar")
	}
}

func newTestDirectory() (root *Directory, err error) {
	if root, err = NewDirectory("gs://bucket/"); err != nil {
		return
	}

	for i := 0; i < 20; i++ {
		obj := storage.Object{Name: fmt.Sprintf("object%d", i)}
		if err = root.AddObject(&obj); err != nil {
			return
		}
	}

	for i := 0; i < 5; i++ {
		obj := storage.Object{Name: fmt.Sprintf("dir%d/object", i)}
		if err = root.AddObject(&obj); err != nil {
			return
		}
	}

	return
}

func BenchmarkBuildIndex(b *testing.B) {
	root, err := newTestDirectory()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err = buildIndex(root, "index.html"); err != nil {
			b.Fatal(err)
		}
	}
}

// For the sake of comparison, see how much faster building the page by
// hand is over the existing implementation using text/template.
// As a bonus this one does escape properly, the existing code doesn't.
func BenchmarkRawBuildIndex(b *testing.B) {
	root, err := newTestDirectory()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawBuildIndex(root, "index.html")
	}
}

func rawBuildIndex(d *Directory, name string) (*storage.Object, io.Reader) {
	title := html.EscapeString(d.Bucket + "/" + d.Prefix)
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	buf.WriteString("<html><head><title>")
	buf.WriteString(title)
	buf.WriteString("</title></head><body><h1>")
	buf.WriteString(title)
	buf.WriteString("</h1>")
	for name := range d.SubDirs {
		buf.WriteString("\n[dir] <a href=\"")
		buf.WriteString(escapePath(name))
		buf.WriteString("\">")
		buf.WriteString(html.EscapeString(name))
		buf.WriteString("</a></br>")
	}
	for name := range d.Objects {
		buf.WriteString("\n[file] <a href=\"")
		buf.WriteString(escapePath(name))
		buf.WriteString("\">")
		buf.WriteString(html.EscapeString(name))
		buf.WriteString("</a></br>")
	}
	buf.WriteString("\n</body></html>\n")

	return &storage.Object{
		Name:         d.Prefix + name,
		ContentType:  "text/html",
		CacheControl: "public, max-age=60",
		Crc32c:       crcSum(buf.Bytes()),
		Size:         uint64(buf.Len()), // used by crcEq but not API
	}, buf
}
