// Copyright 2014 CoreOS, Inc.
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
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"html/template"
	"io"
	"net/http"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"
)

var (
	indexTemplate *template.Template
)

const (
	INDEX_TEXT = `<html>
    <head>
	<title>{{.Bucket}}/{{.Prefix}}</title>
	<meta http-equiv="X-Clacks-Overhead" content="GNU Terry Pratchett" />
    </head>
    <body>
    <h1>{{.Bucket}}/{{.Prefix}}</h1>
    {{range $name, $sub := .SubDirs}}{{if not $sub.Empty}}
	[dir] <a href="{{$name}}/">{{$name}}</a> </br>
    {{end}}{{end}}
    {{range $name, $obj := .Objects}}
	[file] <a href="{{$name}}">{{$name}}</a> </br>
    {{end}}
    </body>
</html>
`
)

func init() {
	indexTemplate = template.Must(template.New("index").Parse(INDEX_TEXT))
}

// Indexer takes a single Directory and updates a single index page.
type Indexer interface {
	Index(d *Directory) error
	Clean(d *Directory) error
}

type WriteMode int

const (
	WriteNever WriteMode = iota
	WriteUpdate
	WriteAlways
)

type basicIndexer struct {
	client *http.Client
	mode   WriteMode
	name   string
}

// NewHtmlIndexer generates "directory/index.html" pages.
func NewHtmlIndexer(client *http.Client, mode WriteMode) Indexer {
	return &basicIndexer{
		client: client,
		mode:   mode,
		name:   "index.html",
	}
}

// NewIndexDirer "directory/" pages.
func NewDirIndexer(client *http.Client, mode WriteMode) Indexer {
	return &basicIndexer{
		client: client,
		mode:   mode,
		name:   "",
	}
}

// crcSum returns the base64 encoded CRC32c sum of the given data
func crcSum(b []byte) string {
	c := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	c.Write(b)
	return base64.StdEncoding.EncodeToString(c.Sum(nil))
}

// Judges whether two Objects are equal based on size and CRC
func crcEq(a, b *storage.Object) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Size == b.Size && a.Crc32c == b.Crc32c
}

func buildIndex(d *Directory, name string) (*storage.Object, io.Reader, error) {
	buf := bytes.Buffer{}
	if err := indexTemplate.Execute(&buf, d); err != nil {
		return nil, nil, err
	}

	obj := storage.Object{
		Name:         d.Prefix + name,
		ContentType:  "text/html",
		CacheControl: "public, max-age=60",
		Crc32c:       crcSum(buf.Bytes()),
		Size:         uint64(buf.Len()), // used by crcEq but not API
	}
	return &obj, &buf, nil
}

func (b *basicIndexer) Index(d *Directory) error {
	// cannot write an object to the bucket root, just skip
	if b.name == "" && d.Prefix == "" {
		return nil
	}

	if d.Empty() {
		return b.Clean(d)
	}

	obj, buf, err := buildIndex(d, b.name)
	if err != nil {
		return err
	}

	if old, ok := d.Indexes[b.name]; ok && b.mode != WriteAlways && crcEq(old, obj) {
		return nil // up to date!
	}

	service, err := storage.New(b.client)
	if err != nil {
		return err
	}

	writeReq := service.Objects.Insert(d.Bucket, obj)
	writeReq.Media(buf)

	if b.mode == WriteNever {
		fmt.Printf("Would write gs://%s/%s\n", d.Bucket, obj.Name)
		return nil
	}
	fmt.Printf("Writing gs://%s/%s\n", d.Bucket, obj.Name)
	_, err = writeReq.Do()
	return wrapError("storage.objects.insert", d.Bucket, obj.Name, err)
}

func (b *basicIndexer) Clean(d *Directory) error {
	// cannot write an object to the bucket root, just skip
	if b.name == "" && d.Prefix == "" {
		return nil
	}

	if _, exists := d.Indexes[b.name]; !exists {
		return nil
	}

	service, err := storage.New(b.client)
	if err != nil {
		return err
	}

	name := d.Prefix + b.name
	if b.mode == WriteNever {
		fmt.Printf("Would delete gs://%s/%s\n", d.Bucket, name)
		return nil
	}
	fmt.Printf("Deleting gs://%s/%s\n", d.Bucket, name)

	delReq := service.Objects.Delete(d.Bucket, name)
	err = delReq.Do()
	return wrapError("storage.objects.delete", d.Bucket, name, err)
}
