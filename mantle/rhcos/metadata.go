// Copyright 2021 Red Hat, Inc.
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

package rhcos

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/coreos/stream-metadata-go/stream"
)

const (
	// If the branch ever renames this will break
	StreamLatest = "main"
)

func fetchURL(u url.URL) ([]byte, error) {
	res, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	return body, nil
}

func getStreamURL(stream string) url.URL {
	u := url.URL{
		Scheme: "https",
		Host:   "raw.githubusercontent.com",
		Path:   "",
	}
	u.Path = fmt.Sprintf("openshift/installer/%s/data/data/rhcos-stream.json", stream)
	return u
}

// FetchStreamMetadata returns a stream
func FetchStreamMetadata(streamName string) (*stream.Stream, error) {
	u := getStreamURL(streamName)
	body, err := fetchURL(u)
	if err != nil {
		return nil, err
	}
	var s *stream.Stream
	if err = json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return s, nil
}

// FetchStreamArtifacts returns a stream artifacts
func FetchStreamArtifacts(stream, architecture string) (*stream.Arch, error) {
	metadata, err := FetchStreamMetadata(stream)
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
	return FetchStreamArtifacts(stream, coreosarch.CurrentRpmArch())
}
