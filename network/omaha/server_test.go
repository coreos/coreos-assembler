// Copyright 2015 CoreOS, Inc.
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

package omaha

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestServerRequestResponse(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	// make an omaha server
	s, err := NewServer(":0")
	if err != nil {
		t.Fatalf("failed to create omaha server: %v", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Serve(); err != nil {
			t.Errorf("Serve failed: %v", err)
		}
	}()

	defer s.Destroy()

	buf := new(bytes.Buffer)
	enc := xml.NewEncoder(buf)
	enc.Indent("", "\t")
	err = enc.Encode(nilRequest)
	if err != nil {
		t.Errorf("failed to marshal request: %v", err)
		return
	}

	// check that server gets the same thing we sent
	rch := s.RequestChan()
	wg.Add(1)
	go func() {
		defer wg.Done()
		sreq, ok := <-rch
		if !ok {
			t.Errorf("failed to get notification from server")
			return
		}

		if err := compareXML(nilRequest, sreq); err != nil {
			t.Error(err)
		}
	}()

	// send omaha request
	endpoint := fmt.Sprintf("http://%s/v1/update/", s.Addr())
	res, err := http.Post(endpoint, "text/xml", buf)
	if err != nil {
		t.Errorf("failed to post: %v", err)
		return
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Errorf("failed to post: %v", res.Status)
		return
	}

	dec := xml.NewDecoder(res.Body)
	sresp := &Response{}
	if err := dec.Decode(sresp); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	if err := compareXML(nilResponse, sresp); err != nil {
		t.Error(err)
	}
}
