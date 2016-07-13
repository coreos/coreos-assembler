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
	"io"
	"net/http"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "network/omaha")

type OmahaHandler struct {
	Updater
}

func (o *OmahaHandler) ServeHTTP(w http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != "POST" {
		plog.Errorf("Unexpected HTTP method: %s", httpReq.Method)
		http.Error(w, "Expected a POST", http.StatusBadRequest)
		return
	}

	// A request over 1M in size is certainly bogus.
	var reader io.Reader
	reader = http.MaxBytesReader(w, httpReq.Body, 1024*1024)

	// Optionally log request bodies.
	if plog.LevelAt(capnslog.DEBUG) {
		pr, pw := io.Pipe()
		go util.LogFrom(capnslog.DEBUG, pr)
		reader = io.TeeReader(reader, pw)
	}

	decoder := xml.NewDecoder(reader)
	var omahaReq Request
	if err := decoder.Decode(&omahaReq); err != nil {
		plog.Errorf("Failed decoding XML: %v", err)
		http.Error(w, "Invalid XML", http.StatusBadRequest)
		return
	}

	if omahaReq.Protocol != "3.0" {
		plog.Errorf("Unexpected protocol: %q", omahaReq.Protocol)
		http.Error(w, "Omaha 3.0 Required", http.StatusBadRequest)
		return
	}

	httpStatus := 0
	omahaResp := NewResponse()
	for _, appReq := range omahaReq.Apps {
		appResp := o.serveApp(omahaResp, httpReq, &omahaReq, appReq)
		if appResp.Status == AppOK {
			// HTTP is ok if any app is ok.
			httpStatus = http.StatusOK
		} else if httpStatus == 0 {
			// If no app is ok HTTP will use the first error.
			if appResp.Status == AppInternalError {
				httpStatus = http.StatusInternalServerError
			} else {
				httpStatus = http.StatusBadRequest
			}
		}
	}

	if httpStatus == 0 {
		httpStatus = http.StatusBadRequest
	}

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(httpStatus)

	// Optionally log response body.
	var writer io.Writer = w
	if plog.LevelAt(capnslog.DEBUG) {
		pr, pw := io.Pipe()
		go util.LogFrom(capnslog.DEBUG, pr)
		writer = io.MultiWriter(w, pw)
	}

	if _, err := writer.Write([]byte(xml.Header)); err != nil {
		plog.Errorf("Failed writing response: %v", err)
		return
	}

	encoder := xml.NewEncoder(writer)
	encoder.Indent("", "\t")
	if err := encoder.Encode(omahaResp); err != nil {
		plog.Errorf("Failed encoding response: %v", err)
	}
}

func (o *OmahaHandler) serveApp(omahaResp *Response, httpReq *http.Request, omahaReq *Request, appReq *AppRequest) *AppResponse {
	if err := o.CheckApp(omahaReq, appReq); err != nil {
		if appStatus, ok := err.(AppStatus); ok {
			return omahaResp.AddApp(appReq.Id, appStatus)
		}
		plog.Error(err)
		return omahaResp.AddApp(appReq.Id, AppInternalError)
	}

	appResp := omahaResp.AddApp(appReq.Id, AppOK)
	if appReq.UpdateCheck != nil {
		o.checkUpdate(appResp, httpReq, omahaReq, appReq)
	}

	if appReq.Ping != nil {
		o.Ping(omahaReq, appReq)
		appResp.AddPing()
	}

	for _, event := range appReq.Events {
		o.Event(omahaReq, appReq, event)
		appResp.AddEvent()
	}

	return appResp
}

func (o *OmahaHandler) checkUpdate(appResp *AppResponse, httpReq *http.Request, omahaReq *Request, appReq *AppRequest) {
	update, err := o.CheckUpdate(omahaReq, appReq)
	if err != nil {
		if updateStatus, ok := err.(UpdateStatus); ok {
			appResp.AddUpdateCheck(updateStatus)
		} else {
			plog.Error(err)
			appResp.AddUpdateCheck(UpdateInternalError)
		}
	} else if update != nil {
		u := appResp.AddUpdateCheck(UpdateOK)
		fillUpdate(u, update, httpReq)
	} else {
		appResp.AddUpdateCheck(NoUpdate)
	}
}

func fillUpdate(u *UpdateResponse, update *Update, httpReq *http.Request) {
	if update.PreviousVersion == "" {
		plog.Infof("Update to %s via full update payload",
			update.Version)
	} else {
		plog.Infof("Update from %s to %s via delta update payload",
			update.PreviousVersion, update.Version)
	}
	u.URLs = update.URLs([]string{"http://" + httpReq.Host})
	u.Manifest = &update.Manifest
}
