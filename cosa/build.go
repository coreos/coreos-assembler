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

package cosa

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/pkg/errors"
)

// Build is the coreos-assembler `meta.json` which defines a build.
// This code was copied from openshift-installer's
// https://github.com/openshift/installer/blob/a0350404997b0493d7bb16aa2875e5c42879b069/pkg/rhcos/builds.go
// For now, copy-paste updates there.  Later, maybe consider making this a public API?
type Build struct {
	Ref           string `json:"ref"`
	OSTreeVersion string `json:"ostree-version"`
	OSTreeCommit  string `json:"ostree-commit"`
	AMIs          []struct {
		Region string `json:"name"`
		HVM    string `json:"hvm"`
	} `json:"amis"`
	Azure struct {
		Image string `json:"image"`
		URL   string `json:"url"`
	}
	GCP struct {
		Image string `json:"image"`
		URL   string `json:"url"`
	}
	BaseURI string `json:"baseURI"`
	Images  struct {
		OSTree struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"ostree"`
		QEMU struct {
			Path               string `json:"path"`
			SHA256             string `json:"sha256"`
			UncompressedSHA256 string `json:"uncompressed-sha256"`
		} `json:"qemu"`
		OpenStack struct {
			Path               string `json:"path"`
			SHA256             string `json:"sha256"`
			UncompressedSHA256 string `json:"uncompressed-sha256"`
		} `json:"openstack"`
	} `json:"images"`
	FedoraCoreOSParentVersion string `json:"fedora-coreos.parent-version"`
}

func ParseBuild(path string) (*Build, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s", path)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	// FIXME enable this to prevent schema regressions https://github.com/coreos/coreos-assembler/pull/1059
	// dec.DisallowUnknownFields()
	var cosaBuild *Build
	if err := dec.Decode(&cosaBuild); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", path)
	}

	return cosaBuild, nil
}

func FetchAndParseBuild(url string) (*Build, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	var cosaBuild *Build
	if err = json.Unmarshal(body, &cosaBuild); err != nil {
		return nil, err
	}

	return cosaBuild, nil
}

func (build *Build) FindAMI(region string) (string, error) {
	for _, ami := range build.AMIs {
		if ami.Region == region {
			return ami.HVM, nil
		}
	}
	return "", fmt.Errorf("no AMI found for region %s", region)
}
