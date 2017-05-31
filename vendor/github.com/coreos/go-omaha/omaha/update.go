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
	"encoding/xml"
)

// Update is a manifest for a single omaha update response. It extends
// the standard Manifest protocol element with the application id and
// previous version which are used to match against the update request.
// A blank previous version indicates this update can be applied to any
// existing install. The application id may not be blank.
type Update struct {
	XMLName         xml.Name `xml:"update" json:"-"`
	ID              string   `xml:"appid,attr"`
	PreviousVersion string   `xml:"previousversion,attr,omitempty"`
	URL             URL      `xml:"urls>url"`
	Manifest

	// The delta_okay request attribute is an update_engine extension.
	RespectDeltaOK bool `xml:"respect_delta_okay,attr,omitempty"`
}

// The URL attribute in Update is currently assumed to be a relative
// path which may be found on multiple mirrors. A server using this is
// expected to know the mirror prefix(s) it can give the client.
func (u *Update) URLs(prefixes []string) []*URL {
	urls := make([]*URL, len(prefixes))
	for i, prefix := range prefixes {
		urls[i] = &URL{CodeBase: prefix + u.URL.CodeBase}
	}
	return urls
}

// Updater provides a common interface for any backend that can respond to
// update requests made to an Omaha server.
type Updater interface {
	CheckApp(req *Request, app *AppRequest) error
	CheckUpdate(req *Request, app *AppRequest) (*Update, error)
	Event(req *Request, app *AppRequest, event *EventRequest)
	Ping(req *Request, app *AppRequest)
}

type UpdaterStub struct{}

func (u UpdaterStub) CheckApp(req *Request, app *AppRequest) error {
	return nil
}

func (u UpdaterStub) CheckUpdate(req *Request, app *AppRequest) (*Update, error) {
	return nil, NoUpdate
}

func (u UpdaterStub) Event(req *Request, app *AppRequest, event *EventRequest) {
	return
}

func (u UpdaterStub) Ping(req *Request, app *AppRequest) {
	return
}
