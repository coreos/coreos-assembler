// Copyright 2020 Red Hat, Inc.
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

package fcos

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const canonicalStreamIndexLocation = "https://builds.coreos.fedoraproject.org/prod/streams/%s/releases.json"

// XXX: dedupe with fedora-coreos-stream-generator

// ReleaseIndex for accessing Release Index metadata
type ReleaseIndex struct {
	Releases []struct {
		Version string `json:"version"`
	} `json:"releases"`
}

func FetchAndParseReleaseIndex(url string) (*ReleaseIndex, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	var index *ReleaseIndex
	if err = json.Unmarshal(body, &index); err != nil {
		return nil, err
	}

	return index, nil
}

func FetchAndParseCanonicalReleaseIndex(stream string) (*ReleaseIndex, error) {
	return FetchAndParseReleaseIndex(fmt.Sprintf(canonicalStreamIndexLocation, stream))
}
