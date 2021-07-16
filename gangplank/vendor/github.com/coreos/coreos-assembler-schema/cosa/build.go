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
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	// ErrMetaFailsValidation is thrown on reading and invalid meta.json
	ErrMetaFailsValidation = errors.New("meta.json failed schema validation")

	// ErrMetaNotFound is thrown when a meta.json cannot be found
	ErrMetaNotFound = errors.New("meta.json was not found")

	// reMetaJSON matches meta.json files use for merging
	reMetaJSON = regexp.MustCompile(`^meta\.(json|.*\.json)$`)

	// forceArch when not empty will override the build arch
	forceArch, _ = os.LookupEnv("COSA_FORCE_ARCH")
)

const (
	// CosaMetaJSON is the meta.json file
	CosaMetaJSON = "meta.json"
)

// SetArch overrides the build arch
func SetArch(a string) {
	forceArch = a
}

// BuilderArch converts the GOARCH to the build arch.
// In other words, it translates amd64 to x86_64.
func BuilderArch() string {
	if forceArch != "" {
		return forceArch
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	return arch
}

// ReadBuild returns a build upon finding a meta.json. Returns a Build, the path string
// to the build, and an error (if any). If the buildID is not set, "latest" is assumed.
func ReadBuild(dir, buildID, arch string) (*Build, string, error) {
	if arch == "" {
		arch = BuilderArch()
	}

	if buildID == "" {
		b, err := getBuilds(dir)
		if err != nil {
			return nil, "", err
		}
		latest, ok := b.getLatest(arch)
		if !ok {
			return nil, "", ErrNoBuildsFound
		}
		buildID = latest
	}

	if buildID == "" {
		return nil, "", fmt.Errorf("build is undefined")
	}

	p := filepath.Join(dir, buildID, arch)
	f, err := Open(filepath.Join(p, CosaMetaJSON))
	if err != nil {
		return nil, "", fmt.Errorf("failed to open %s to read meta.json: %w", p, err)
	}

	b, err := buildParser(f)
	if err != nil {
		return b, p, err
	}

	// if delaydMetaMerge is set, then we need to load up and merge and meta.*.json
	// into the memory model of the build
	if b != nil && b.CosaDelayedMetaMerge {
		log.Info("Searching for extra meta.json files")
		files := walkFn(p)
		for {
			fi, ok := <-files
			if !ok {
				break
			}
			if fi == nil || fi.IsDir() || fi.Name() == CosaMetaJSON {
				continue
			}
			if !IsMetaJSON(fi.Name()) {
				continue
			}
			log.WithField("extra meta.json", fi.Name()).Info("found meta")
			f, err := Open(filepath.Join(p, fi.Name()))
			if err != nil {
				return b, p, err
			}
			defer f.Close()
			if err := b.mergeMeta(f); err != nil {
				return b, p, err
			}
		}
	}

	return b, p, err
}

func buildParser(r io.Reader) (*Build, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var cosaBuild *Build
	if err := dec.Decode(&cosaBuild); err != nil {
		return nil, errors.Wrapf(err, "failed to parse build")
	}
	return cosaBuild, nil
}

// ParseBuild parses the meta.json and reutrns a build
func ParseBuild(path string) (*Build, error) {
	f, err := Open(path)
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

// WriteMeta records the meta-data. Writes are local only.
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
	if ok && r.Path != "" {
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

// GetCommandBuildableArtifacts returns the string name of buildable artifacts
// through the `cosa build-*` CLI.
func GetCommandBuildableArtifacts() []string {
	b := new(Build)
	b.BuildArtifacts = new(BuildArtifacts)
	// 'extensions' is a special case artifact that exists outside the images
	ret := []string{"extensions"}
	liveAdded := false
	for k := range b.artifacts() {
		switch k {
		case "ostree":
			continue
		case "kernel", "initramfs":
			continue
		case "iso", "live-iso", "live-kernel", "live-initramfs", "live-rootfs":
			if !liveAdded {
				ret = append(ret, "live")
				liveAdded = true
			}
		default:
			ret = append(ret, k)
		}
	}
	return sort.StringSlice(ret)
}

// artifact returns a string map of artifacts, where the key
// is the JSON tag. Reflection was over a case statement to make meta.json
// and the schema authoritative for adding and removing artifacts.
func (build *Build) artifacts() map[string]*Artifact {
	ret := make(map[string]*Artifact)

	// Special case Extensions
	// technically extentions are an artifact, albeit the meta-data located
	// as a top level entry.
	ret["extensions"] = build.Extensions.toArtifact()

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

// mergeMeta uses JSON to merge in the data
func (b *Build) mergeMeta(r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(b)
}

// IsMetaJSON is a helper for identifying if a file is meta.json
func IsMetaJSON(path string) bool {
	b := filepath.Base(path)
	return reMetaJSON.Match([]byte(b))
}

// toArtifact converts an Extension to an Artifact
func (e *Extensions) toArtifact() *Artifact {
	if e == nil {
		return new(Artifact)
	}
	return &Artifact{
		Path:   e.Path,
		Sha256: e.Sha256,
	}
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

func (build *Build) FindGCPImage() (string, error) {
	if build.Gcp != nil {
		project := build.Gcp.ImageProject
		if project == "" {
			// Hack for when meta.json didn't include the project. We can
			// probably drop this in the future. See:
			// https://github.com/coreos/coreos-assembler/pull/1335
			project = "fedora-coreos-cloud"
		}
		return fmt.Sprintf("projects/%s/global/images/%s", project, build.Gcp.ImageName), nil
	}
	return "", errors.New("no GCP image found")
}
