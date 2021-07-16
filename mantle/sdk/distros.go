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

	"github.com/coreos/coreos-assembler-schema/cosa"
)

// TargetDistroFromName returns the distribution given
// the path to an artifact (usually a disk image).
func TargetDistroFromName(artifact string) string {
	basename := filepath.Base(artifact)
	if strings.HasPrefix(basename, "rhcos-") {
		return "rhcos"
	}
	// For now, just assume fcos
	return "fcos"
}

// TargetDistro returns the distribution of a cosa build
func TargetDistro(build *cosa.Build) (string, error) {
	switch build.Name {
	case "rhcos":
		return "rhcos", nil
	case "fedora-coreos":
		return "fcos", nil
	default:
		return "", fmt.Errorf("Unknown distribution: %s", build.Name)
	}
}
