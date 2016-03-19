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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"
)

type redirector struct {
	client *http.Client
	mode   WriteMode
}

// NewRedirector generates "directory" pages to redirect to "directory/"
func NewRedirector(client *http.Client, mode WriteMode) Indexer {
	return &redirector{client: client, mode: mode}
}

func escapePath(path string) string {
	u := url.URL{Path: path}
	return u.EscapedPath()
}

func buildRedirect(d *Directory) (*storage.Object, io.Reader) {
	name := strings.TrimSuffix(d.Prefix, "/")
	link := escapePath(path.Base(name))
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	buf.WriteString("<html><head>\n")
	// TODO: include <link rel="canonical" href="d.Prefix"/>
	// I suspect that's only meaningful if we switch to absolute paths
	buf.WriteString(`<meta http-equiv="refresh" content="0;url=`)
	buf.WriteString(link)
	buf.WriteString("/\">\n</head></html>\n")

	return &storage.Object{
		Name:         name,
		ContentType:  "text/html",
		CacheControl: "public, max-age=60",
		Crc32c:       crcSum(buf.Bytes()),
		Size:         uint64(buf.Len()), // used by crcEq but not API
	}, buf
}

func (r *redirector) Index(d *Directory) error {
	// not applicable for the bucket root
	if d.Prefix == "" {
		return nil
	}

	if len(d.SubDirs) == 0 && len(d.Objects) == 0 {
		return nil
	}

	obj, buf := buildRedirect(d)
	if r.mode != WriteAlways && crcEq(d.Redirect, obj) {
		return nil // up to date!
	}

	service, err := storage.New(r.client)
	if err != nil {
		return err
	}

	writeReq := service.Objects.Insert(d.Bucket, obj)
	writeReq.Media(buf)

	if r.mode == WriteNever {
		fmt.Printf("Would write gs://%s/%s\n", d.Bucket, obj.Name)
		return nil
	}
	fmt.Printf("Writing gs://%s/%s\n", d.Bucket, obj.Name)
	_, err = writeReq.Do()
	return err
}
