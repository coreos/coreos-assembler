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

package sdk

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/cosa"
)

// TargetIgnitionVersionFromName returns the Ignition spec version that should
// be provided to a given OS, as identified by a string that can
// be a disk image or the "name" of a coreos-assembler stream.
func TargetIgnitionVersionFromName(artifact string) string {
	basename := filepath.Base(artifact)
	ignition_spec2_openshift_releases := []int{1, 2, 3, 4, 5}
	// The output from the RHCOS pipeline names images like
	// rhcos-42.81.$datestamp.  The images are renamed when
	// placed at e.g. https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.2/4.2.0/
	for _, v := range ignition_spec2_openshift_releases {
		if strings.HasPrefix(basename, fmt.Sprintf("rhcos-4%d", v)) ||
			strings.HasPrefix(basename, fmt.Sprintf("rhcos-4.%d", v)) {
			return "v2"
		}
	}
	return "v3"
}

func TargetIgnitionVersion(build *cosa.Build) string {
	if build.BuildArtifacts == nil {
		panic("TargetIgnitionVersion couldn't find artifact")
	}
	// Most cosa builds should have an "ostree"
	return TargetIgnitionVersionFromName(build.BuildArtifacts.Ostree.Path)
}
