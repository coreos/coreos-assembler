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

// This models the release index:
// https://github.com/coreos/fedora-coreos-tracker/tree/master/metadata/release-index
type ReleaseIndex struct {
	Note     string                `json:"note"` // used to note to users not to consume the release metadata index
	Releases []ReleaseIndexRelease `json:"releases"`
	Metadata ReleaseIndexMetadata  `json:"metadata"`
	Stream   string                `json:"stream"`
}

type ReleaseIndexRelease struct {
	CommitHash []ReleaseCommit `json:"commits"`
	Version    string          `json:"version"`
	Endpoint   string          `json:"metadata"`
}

type ReleaseIndexMetadata struct {
	LastModified string `json:"last-modified"`
}

// This models release metadata:
// https://github.com/coreos/fedora-coreos-tracker/tree/master/metadata/release
type Release struct {
	Architectures map[string]ReleaseArchitecture `json:"architectures"`
}

type ReleaseArchitecture struct {
	Commit string                  `json:"commit"`
	Media  map[string]ReleaseMedia `json:"media"`
}

type ReleaseMedia struct {
	Images map[string]ReleaseAMI `json:"images"`
}

type ReleaseAMI struct {
	Image string `json:"image"`
}

type ReleaseCommit struct {
	Architecture string `json:"architecture"`
	Checksum     string `json:"checksum"`
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
