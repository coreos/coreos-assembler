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
	"os"
	"testing"
)

func mkUpdateReq() (*bytes.Buffer, error) {
	req := NewRequest()
	app := req.AddApp(testAppId, testAppVer)
	app.AddUpdateCheck()

	buf := &bytes.Buffer{}
	enc := xml.NewEncoder(buf)
	enc.Indent("", "\t")
	if err := enc.Encode(req); err != nil {
		return nil, err
	}

	return buf, nil
}

func TestTrivialServer(t *testing.T) {
	tmp, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("test"); err != nil {
		t.Fatal(err)
	}

	s, err := NewTrivialServer(":0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy()
	if err := s.SetPackage(tmp.Name()); err != nil {
		t.Fatal(err)
	}
	go s.Serve()

	buf, err := mkUpdateReq()
	if err != nil {
		t.Fatal(err)
	}

	endpoint := fmt.Sprintf("http://%s/v1/update/", s.Addr())
	res, err := http.Post(endpoint, "text/xml", buf)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("failed to post: %v", res.Status)
	}

	dec := xml.NewDecoder(res.Body)
	resp := &Response{}
	if err := dec.Decode(resp); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}

	if len(resp.Apps) != 1 ||
		resp.Apps[0].UpdateCheck == nil ||
		resp.Apps[0].UpdateCheck.Status != UpdateOK ||
		len(resp.Apps[0].UpdateCheck.URLs) != 1 ||
		resp.Apps[0].UpdateCheck.Manifest == nil ||
		len(resp.Apps[0].UpdateCheck.Manifest.Packages) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}

	pkgres, err := http.Get(resp.Apps[0].UpdateCheck.URLs[0].CodeBase +
		resp.Apps[0].UpdateCheck.Manifest.Packages[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	pkgdata, err := ioutil.ReadAll(pkgres.Body)
	pkgres.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	if string(pkgdata) != "test" {
		t.Fatalf("unexpected package data: %q", string(pkgdata))
	}
}
