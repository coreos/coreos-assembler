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
	"net/http"
)

// Arbitrary limit on the number of concurrent calls to WriteIndex
const MAX_WRITERS = 12

func Update(client *http.Client, url string) error {
	root, err := NewDirectory(url)
	if err != nil {
		return err
	}

	if err = root.Fetch(client); err != nil {
		return err
	}

	indexers := []Indexer{
		NewHtmlIndexer(client),
		NewDirIndexer(client),
	}
	dirs := make(chan *Directory)
	done := make(chan struct{})
	errc := make(chan error)

	// Feed the directory tree into the writers.
	go func() {
		root.Walk(dirs)
		close(dirs)
	}()

	writer := func() {
		for {
			select {
			case d, ok := <-dirs:
				if !ok {
					errc <- nil
					return
				}
				for _, ix := range indexers {
					if err := ix.Index(d); err != nil {
						errc <- err
						return
					}
				}
			case <-done:
				errc <- nil
				return
			}
		}
	}

	for i := 0; i < MAX_WRITERS; i++ {
		go writer()
	}

	// Wait for writers to finish, aborting and returning the first error.
	var ret error
	for i := 0; i < MAX_WRITERS; i++ {
		err := <-errc
		if err == nil {
			continue
		}
		if done != nil {
			close(done)
			done = nil
		}
		if ret == nil {
			ret = err
		}
	}

	return ret
}
