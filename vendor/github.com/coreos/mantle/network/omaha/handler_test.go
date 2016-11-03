// Copyright 2013-2015 CoreOS, Inc.
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
	"encoding/xml"
	"fmt"
	"testing"

	"github.com/kylelemons/godebug/diff"
)

const (
	testAppId  = "{27BD862E-8AE8-4886-A055-F7F1A6460627}"
	testAppVer = "1.0.0"
)

var (
	nilRequest  *Request
	nilResponse *Response
)

func init() {
	nilRequest = NewRequest()
	nilRequest.AddApp(testAppId, testAppVer)
	nilResponse = NewResponse()
	nilResponse.AddApp(testAppId, AppOK)
}

func compareXML(a, b interface{}) error {
	aXml, err := xml.MarshalIndent(a, "", "\t")
	if err != nil {
		return err
	}

	bXml, err := xml.MarshalIndent(b, "", "\t")
	if err != nil {
		return err
	}

	if d := diff.Diff(string(aXml), string(bXml)); d != "" {
		err := fmt.Errorf("Unexpected XML:\n%s", d)
		return err
	}

	return nil
}

func TestHandleNilRequest(t *testing.T) {
	handler := OmahaHandler{UpdaterStub{}}
	response := NewResponse()
	handler.serveApp(response, nil, nilRequest, nilRequest.Apps[0])
	if err := compareXML(nilResponse, response); err != nil {
		t.Error(err)
	}
}
