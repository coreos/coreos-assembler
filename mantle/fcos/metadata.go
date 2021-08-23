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
	"net/url"

	"github.com/coreos/stream-metadata-go/fedoracoreos"
	fcosinternals "github.com/coreos/stream-metadata-go/fedoracoreos/internals"
	"github.com/coreos/stream-metadata-go/release"
	"github.com/coreos/stream-metadata-go/stream"

	"github.com/coreos/mantle/system"
)

func fetchURL(u url.URL) ([]byte, error) {
	res, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	return body, nil
}

// FetchAndParseCanonicalReleaseIndex returns a release index
func FetchAndParseCanonicalReleaseIndex(stream string) (*release.Index, error) {
	url := fcosinternals.GetReleaseIndexURL(stream)
	body, err := fetchURL(url)
	if err != nil {
		return nil, err
	}

	var index *release.Index
	if err = json.Unmarshal(body, &index); err != nil {
		return nil, err
	}

	return index, nil
}

// FetchAndParseCanonicalStreamMetadata returns a stream
func FetchAndParseCanonicalStreamMetadata(streamName string) (*stream.Stream, error) {
	url := fedoracoreos.GetStreamURL(streamName)
	body, err := fetchURL(url)
	if err != nil {
		return nil, err
	}

	var s *stream.Stream
	if err = json.Unmarshal(body, &s); err != nil {
		return nil, err
	}

	return s, nil
}

// FetchCanonicalStreamArtifacts returns a stream artifacts
func FetchCanonicalStreamArtifacts(stream, architecture string) (*stream.Arch, error) {
	metadata, err := FetchAndParseCanonicalStreamMetadata(stream)
	if err != nil {
		return nil, fmt.Errorf("fetching stream metadata: %v", err)
	}
	arch, ok := metadata.Architectures[architecture]
	if !ok {
		return nil, fmt.Errorf("stream metadata missing architecture %q", architecture)
	}
	return &arch, nil
}

// FetchStreamThisArchitecture returns artifacts for the current architecture from
// the given stream.
func FetchStreamThisArchitecture(stream string) (*stream.Arch, error) {
	return FetchCanonicalStreamArtifacts(stream, system.RpmArch())
}

// GetCosaBuildURL returns a URL prefix for the coreos-assembler build.
func GetCosaBuildURL(stream, buildid, arch string) string {
	u := fcosinternals.GetCosaBuild(stream, buildid, arch)
	return u.String()
}
