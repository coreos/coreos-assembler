// Copyright 2016 CoreOS, Inc.
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
	"net/http"
)

type trivialUpdater struct {
	UpdaterStub
	Update
}

func (tu *trivialUpdater) CheckUpdate(req *Request, app *AppRequest) (*Update, error) {
	return &tu.Update, nil
}

type trivialHandler struct {
	Path string
}

func (th *trivialHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if th.Path == "" {
		http.NotFound(w, r)
	}
	http.ServeFile(w, r, th.Path)
}

type TrivialServer struct {
	*Server
	th trivialHandler
}

func NewTrivialServer(addr string) (*TrivialServer, error) {
	s, err := NewServer(addr, &UpdaterStub{})
	if err != nil {
		return nil, err
	}
	ts := TrivialServer{Server: s}
	ts.Mux.Handle("/packages/update.gz", &ts.th)
	return &ts, nil
}

func (ts *TrivialServer) SetPackage(path string) error {
	tu := trivialUpdater{
		Update: Update{
			URL: URL{CodeBase: "/packages/"},
		},
	}

	pkg, err := tu.Manifest.AddPackageFromPath(path)
	if err != nil {
		return err
	}
	pkg.Name = "update.gz"
	act := tu.Manifest.AddAction("postinstall")
	act.DisablePayloadBackoff = true
	act.Sha256 = pkg.Sha256

	ts.th.Path = path
	ts.Updater = &tu
	return nil
}
