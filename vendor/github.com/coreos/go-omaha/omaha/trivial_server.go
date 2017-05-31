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
	"fmt"
	"net/http"
	"path"
)

const pkg_prefix = "/packages/"

// trivialUpdater always responds with the given Update.
type trivialUpdater struct {
	UpdaterStub
	Update
}

func (tu *trivialUpdater) CheckUpdate(req *Request, app *AppRequest) (*Update, error) {
	if len(tu.Manifest.Packages) == 0 {
		return nil, NoUpdate
	}
	return &tu.Update, nil
}

// trivialHandler serves up a single file.
type trivialHandler struct {
	Path string
}

func (th *trivialHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if th.Path == "" {
		http.NotFound(w, r)
	}
	http.ServeFile(w, r, th.Path)
}

// TrivialServer is an extremely basic Omaha server that ignores all
// incoming metadata, always responding with the same update response.
// The update is constructed by calling AddPackage one or more times.
type TrivialServer struct {
	*Server
	tu trivialUpdater
}

func NewTrivialServer(addr string) (*TrivialServer, error) {
	ts := TrivialServer{
		tu: trivialUpdater{
			Update: Update{
				URL: URL{CodeBase: pkg_prefix},
			},
		},
	}

	s, err := NewServer(addr, &ts.tu)
	if err != nil {
		return nil, err
	}
	ts.Server = s

	return &ts, nil
}

// AddPackage adds a new file to the update response.
// file is the local filesystem path, name is the final URL component.
func (ts *TrivialServer) AddPackage(file, name string) error {
	// name may not include any path components
	if path.Base(name) != name || name[0] == '.' {
		return fmt.Errorf("invalid package name %q", name)
	}

	pkg, err := ts.tu.Manifest.AddPackageFromPath(file)
	if err != nil {
		return err
	}
	pkg.Name = name

	// Insert the update_engine style postinstall action if
	// this is the first (and probably only) package.
	if len(ts.tu.Manifest.Actions) == 0 {
		act := ts.tu.Manifest.AddAction("postinstall")
		act.DisablePayloadBackoff = true
		act.SHA256 = pkg.SHA256
	}

	ts.Mux.Handle(pkg_prefix+name, &trivialHandler{file})
	return nil
}
