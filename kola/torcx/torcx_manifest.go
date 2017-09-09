// Copyright 2017 CoreOS, Inc.
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

package torcx

import (
	"encoding/json"
	"errors"
)

const packageListKind = "torcx-package-list-v0"

type Manifest struct {
	Packages []Package
}

// a copy of the Manifest type with no UnmarshalJSON method so the below doesn't do a round of recursion
type torcxManifestUnmarshalValue struct {
	Packages []Package
}

func (t *Manifest) UnmarshalJSON(b []byte) error {
	if t == nil {
		return errors.New("Unmarshal(nil *Manifest)")
	}
	wrappingType := struct {
		Kind  string                      `json:"kind"`
		Value torcxManifestUnmarshalValue `json:"value"`
	}{}

	if err := json.Unmarshal(b, &wrappingType); err != nil {
		return err
	}
	if wrappingType.Kind != packageListKind {
		return errors.New("Unrecognized torcx packagelist kind: " + wrappingType.Kind)
	}

	t.Packages = wrappingType.Value.Packages
	return nil
}

type Package struct {
	Name           string
	DefaultVersion *string
	Versions       []Version
}

type Version struct {
	Version       string
	Hash          string
	CasDigest     string
	SourcePackage string
	Locations     []Location
}

type Location struct {
	Path *string
	URL  *string
}
