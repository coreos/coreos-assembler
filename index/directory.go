/*
   Copyright 2014 CoreOS, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package index

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/code.google.com/p/google-api-go-client/storage/v1"
)

type Directory struct {
	Bucket  string
	Prefix  string
	SubDirs map[string]*Directory
	Objects map[string]*storage.Object
	Updated time.Time
}

func NewDirectory(rawURL string) (*Directory, error) {
	gsURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if gsURL.Scheme != "gs" {
		return nil, fmt.Errorf("URL missing gs:// scheme prefix: %q", rawURL)
	}
	if gsURL.Host == "" {
		return nil, fmt.Errorf("URL missing bucket name: %q", rawURL)
	}

	// Object name prefix must never start with / but always end with /
	gsURL.Path = strings.TrimLeft(gsURL.Path, "/")
	if gsURL.Path != "" && !strings.HasSuffix(gsURL.Path, "/") {
		gsURL.Path += "/"
	}

	return &Directory{
		Bucket:  gsURL.Host,
		Prefix:  gsURL.Path,
		SubDirs: make(map[string]*Directory),
		Objects: make(map[string]*storage.Object),
	}, nil
}

func (d *Directory) Fetch(client *http.Client) error {
	service, err := storage.New(client)
	if err != nil {
		return err
	}

	fmt.Printf("Fetching gs://%s/%s\n", d.Bucket, d.Prefix)
	objCount := 0
	listReq := service.Objects.List(d.Bucket)
	if d.Prefix != "" {
		listReq.Prefix(d.Prefix)
	}

	for {
		listRes, err := listReq.Do()
		if err != nil {
			return err
		}

		objCount += len(listRes.Items)
		fmt.Printf("Found %d objects under gs://%s/%s\n",
			objCount, d.Bucket, d.Prefix)
		for _, obj := range listRes.Items {
			if err := d.AddObject(obj); err != nil {
				return err
			}
		}

		if listRes.NextPageToken != "" {
			listReq.PageToken(listRes.NextPageToken)
		} else {
			break
		}
	}

	return nil
}

func (d *Directory) AddObject(obj *storage.Object) error {
	name := strings.TrimPrefix(obj.Name, d.Prefix)
	split := strings.SplitAfterN(name, "/", 2)

	// Propagate update time to parent directories, excluding indexes.
	// Used to detect when indexes should be regenerated.
	if split[len(split)-1] != "index.html" {
		objUpdated, err := time.Parse(time.RFC3339Nano, obj.Updated)
		if err != nil {
			return err
		}
		if d.Updated.Before(objUpdated) {
			d.Updated = objUpdated
		}
	}

	// Save object locally if it has no slash or only ends in slash
	if len(split) == 1 || len(split[1]) == 0 {
		d.Objects[name] = obj
		return nil
	}

	sub, ok := d.SubDirs[split[0]]
	if !ok {
		sub = &Directory{
			Bucket:  d.Bucket,
			Prefix:  d.Prefix + split[0],
			SubDirs: make(map[string]*Directory),
			Objects: make(map[string]*storage.Object),
		}
		d.SubDirs[split[0]] = sub
	}

	return sub.AddObject(obj)
}

func (d *Directory) NeedsIndex() bool {
	if len(d.SubDirs) == 0 && len(d.Objects) == 0 {
		return false
	}
	if index, ok := d.Objects["index.html"]; ok {
		indexUpdated, err := time.Parse(time.RFC3339Nano, index.Updated)
		return err != nil || d.Updated.After(indexUpdated)
	}
	return true
}

func (d *Directory) Walk(dirs chan<- *Directory) {
	dirs <- d
	for _, subdir := range d.SubDirs {
		subdir.Walk(dirs)
	}
}
