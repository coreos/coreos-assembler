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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/pkg/errors"
)

var (
	// ErrMetaFailsValidation is thrown on reading and invalid meta.json
	ErrMetaFailsValidation = errors.New("meta.json failed schema validation")

	// ErrMetaNotFound is thrown when a meta.json cannot be found
	ErrMetaNotFound = errors.New("meta.json was not found")
)

const (
	// CosaMetaJSON is the meta.json file
	CosaMetaJSON = "meta.json"
)

// BuilderArch converts the GOARCH to the build arch.
// In other words, it translates amd64 to x86_64.
func BuilderArch() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	return arch
}

// ReadBuild returns a build upon finding a meta.json.
// If build is "", use latest
func ReadBuild(dir, buildID, arch string) (*Build, string, error) {
	if arch == "" {
		arch = BuilderArch()
	}

	if buildID == "" {
		b, err := getBuilds(dir)
		if err == nil {
			latest, ok := b.getLatest(arch)
			if !ok {
				return nil, "", ErrNoBuildsFound
			}
			buildID = latest
		}
	}

	if buildID == "" {
		return nil, "", fmt.Errorf("build is undefined")
	}

	p := filepath.Join(dir, "builds", buildID, arch)
	f, err := os.Open(filepath.Join(p, CosaMetaJSON))
	if err != nil {
		return nil, "", fmt.Errorf("failed to open %s to read meta.json: %w", p, err)
	}

	b, err := buildParser(f)
	return b, p, err
}

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

// ParseBuild parses the meta.json and reutrns a build
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

// WriteMeta records the meta-data
func (build *Build) WriteMeta(path string, validate bool) error {
	if validate {
		if err := build.Validate(); len(err) != 0 {
			return errors.New("data is not compliant with schema")
		}
	}
	out, err := json.MarshalIndent(build, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, out, 0644)
}

// GetArtifact returns an artifact by JSON tag
func (build *Build) GetArtifact(artifact string) (*Artifact, error) {
	r, ok := build.artifacts()[artifact]
	if ok {
		return r, nil
	}
	return nil, errors.New("artifact not defined")
}

// IsArtifact takes a path and returns the artifact type and a bool if
// the artifact is described in the build.
func (build *Build) IsArtifact(path string) (string, bool) {
	path = filepath.Base(path)
	for k, v := range build.artifacts() {
		if v.Path == path {
			return k, true
		}
	}
	return "", false
}

// CanArtifact reports whether an artifact name is buildable by COSA based
// on the meta.json name. CanArtifact is used to signal if the artifact is a known
// artifact type.
func CanArtifact(artifact string) bool {
	b := new(Build)
	b.BuildArtifacts = new(BuildArtifacts)
	_, ok := b.artifacts()[artifact]
	return ok
}

// artifact returns a string map of artifacts, where the key
// is the JSON tag. Reflection was over a case statement to make meta.json
// and the schema authoritative for adding and removing artifacts.
func (build *Build) artifacts() map[string]*Artifact {
	ret := make(map[string]*Artifact)
	var ba BuildArtifacts = *build.BuildArtifacts
	rv := reflect.TypeOf(ba)
	for i := 0; i < rv.NumField(); i++ {
		tag := rv.Field(i).Tag.Get("json")
		tag = strings.Split(tag, ",")[0]
		field := reflect.ValueOf(&ba).Elem().Field(i)

		// If the field is zero, then we create a stub artifact.
		if field.IsZero() {
			ret[strings.ToLower(tag)] = &Artifact{}
			continue
		}

		// When the json struct tag does not have "omitempty"
		// then we get an actual struct not the pointer.
		if field.Kind() == reflect.Struct {
			r, ok := field.Interface().(Artifact)
			if ok {
				ret[strings.ToLower(tag)] = &r
			}
			continue
		}

		// Optional structs (i.e. "omitempty") are pointers a struct.
		if field.Addr().Elem().CanInterface() {
			r, ok := reflect.ValueOf(&ba).Elem().Field(i).Elem().Interface().(Artifact)
			if ok {
				ret[strings.ToLower(tag)] = &r
			}
		}
	}
	return ret
}
