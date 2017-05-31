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
	"log"
	"net/http"
)

type OmahaHandler struct {
	Updater
}

func (o *OmahaHandler) ServeHTTP(w http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != "POST" {
		log.Printf("omaha: Unexpected HTTP method: %s", httpReq.Method)
		http.Error(w, "Expected a POST", http.StatusBadRequest)
		return
	}

	// A request over 1M in size is certainly bogus.
	reader := http.MaxBytesReader(w, httpReq.Body, 1024*1024)
	contentType := httpReq.Header.Get("Content-Type")
	omahaReq, err := ParseRequest(contentType, reader)
	if err != nil {
		log.Printf("omaha: Failed parsing request: %v", err)
		http.Error(w, "Bad Omaha Request", http.StatusBadRequest)
		return
	}

	httpStatus := 0
	omahaResp := NewResponse()
	for _, appReq := range omahaReq.Apps {
		appResp := o.serveApp(omahaResp, httpReq, omahaReq, appReq)
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

	if _, err := w.Write([]byte(xml.Header)); err != nil {
		log.Printf("omaha: Failed writing response: %v", err)
		return
	}

	encoder := xml.NewEncoder(w)
	encoder.Indent("", "\t")
	if err := encoder.Encode(omahaResp); err != nil {
		log.Printf("omaha: Failed encoding response: %v", err)
	}
}

func (o *OmahaHandler) serveApp(omahaResp *Response, httpReq *http.Request, omahaReq *Request, appReq *AppRequest) *AppResponse {
	if err := o.CheckApp(omahaReq, appReq); err != nil {
		if appStatus, ok := err.(AppStatus); ok {
			return omahaResp.AddApp(appReq.ID, appStatus)
		}
		log.Printf("omaha: CheckApp failed: %v", err)
		return omahaResp.AddApp(appReq.ID, AppInternalError)
	}

	appResp := omahaResp.AddApp(appReq.ID, AppOK)
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
			log.Printf("omaha: CheckUpdate failed: %v", err)
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
	u.URLs = update.URLs([]string{"http://" + httpReq.Host})
	u.Manifest = &update.Manifest
}
