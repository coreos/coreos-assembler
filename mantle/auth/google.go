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

// Package auth provides Google oauth2 and Azure credential bindings for mantle.
package auth

import (
	"context"
	"net/http"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const GCEConfigPath = ".config/gce.json"

var scopes = []string{
	"https://www.googleapis.com/auth/devstorage.full_control",
	"https://www.googleapis.com/auth/compute",
}

// GoogleServiceClient fetchs a token from Google Compute Engine's
// metadata service. This should be used on GCE vms. The Default account
// is used.
func GoogleServiceClient() *http.Client {
	return &http.Client{
		Transport: &oauth2.Transport{
			Source: google.ComputeTokenSource(""),
		},
	}
}

// GoogleServiceTokenSource provides an oauth2.TokenSource authorized in
// the same manner as GoogleServiceClient().
func GoogleServiceTokenSource() oauth2.TokenSource {
	return google.ComputeTokenSource("")
}

func GoogleClientFromKeyFile(path string, scope ...string) (*http.Client, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, GCEConfigPath)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return GoogleClientFromJSONKey(b, scope...)
}

// GoogleClientFromJSONKey  provides an http.Client authorized with an
// oauth2 token retrieved using a Google Developers service account's
// private JSON key file.
func GoogleClientFromJSONKey(jsonKey []byte, scope ...string) (*http.Client, error) {
	if scope == nil {
		scope = scopes
	}
	jwtConf, err := google.JWTConfigFromJSON(jsonKey, scope...)
	if err != nil {
		return nil, err
	}

	return jwtConf.Client(context.TODO()), nil

}

// GoogleTokenSourceFromJSONKey provides an oauth2.TokenSource
// authorized in the same manner as GoogleClientFromJSONKey.
func GoogleTokenSourceFromJSONKey(jsonKey []byte, scope ...string) (oauth2.TokenSource, error) {
	if scope == nil {
		scope = scopes
	}

	jwtConf, err := google.JWTConfigFromJSON(jsonKey, scope...)
	if err != nil {
		return nil, err
	}

	return jwtConf.TokenSource(context.TODO()), nil
}
