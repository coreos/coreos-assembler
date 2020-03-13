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
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/pkg/errors"
)

var (
	// ErrMetaFailsValidation is thrown on reading and invalid meta.json
	ErrMetaFailsValidation = errors.New("meta.json failed schema validation")
)

func buildParser(r io.Reader) (*Build, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var cosaBuild *Build
	if err := dec.Decode(&cosaBuild); err != nil {
		return nil, errors.Wrapf(err, "failed to parse build")
	}
	if errs := cosaBuild.Validate(); len(errs) > 0 {
		return nil, errors.Wrapf(ErrMetaFailsValidation, "%v", errs)
	}
	return cosaBuild, nil
}

func ParseBuild(path string) (*Build, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s", path)
	}
	defer f.Close()
	b, err := buildParser(f)
	if err != nil {
		return nil, errors.Wrapf(err, "failed parsing of %s", path)
	}
	return b, err
}

func FetchAndParseBuild(url string) (*Build, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return buildParser(res.Body)
}

func (build *Build) FindAMI(region string) (string, error) {
	for _, ami := range build.Amis {
		if ami.Region == region {
			return ami.Hvm, nil
		}
	}
	return "", fmt.Errorf("no AMI found for region %s", region)
}

func (build *Build) WriteMeta(path string, validate bool) error {
	if validate {
		if err := build.Validate(); len(err) != 0 {
			errors.New("data is not compliant with schema")
		}
	}
	out, err := json.MarshalIndent(build, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, out, 0644)
}
