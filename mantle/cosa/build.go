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
	for _, ami := range build.Amis {
		if ami.Region == region {
			return ami.Hvm, nil
		}
	}
	return "", fmt.Errorf("no AMI found for region %s", region)
}
