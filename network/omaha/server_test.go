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
	"io/ioutil"
	"net/http"
	"sync"
	"testing"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/kylelemons/godebug/pretty"
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

	defer s.Stop()

	// make an omaha request
	request := NewRequest()

	// ensures the struct is the same as what appears out of the Decoder in
	// Server's handler
	request.XMLName.Local = "request"

	request.OS.Platform = "CoreOS"

	buf := new(bytes.Buffer)
	enc := xml.NewEncoder(buf)
	enc.Indent("", "\t")
	err = enc.Encode(request)
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

		if diff := pretty.Compare(request, sreq); diff != "" {
			t.Errorf("client request differs from what server got: %v", diff)
		}
	}()

	// send omaha request
	endpoint := fmt.Sprintf("http://%s/v1/update/", s.Addr())
	t.Logf("sending request to %q:\n%s\n", endpoint, buf)
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

	body, _ := ioutil.ReadAll(res.Body)

	t.Logf("got response:\n%s\n", body)

}
