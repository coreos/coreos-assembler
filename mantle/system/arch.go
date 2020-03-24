// Copyright 2015 CoreOS, Inc.
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

package system

import (
	"fmt"
	"runtime"
)

// RpmArch returns the architecture in RPM terms.
func RpmArch() string {
	goarch := runtime.GOARCH
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "ppc64le", "s390x":
		return goarch
	default:
		panic(fmt.Sprintf("RpmArch: No mapping defined for GOARCH %s", goarch))
	}
}

func PortageArch() string {
	arch := runtime.GOARCH
	switch arch {
	case "386":
		arch = "x86"

	// Go and Portage agree for these.
	case "amd64":
	case "arm":
	case "arm64":
	case "ppc64":
	case "s390x":
	case "ppc64le":
	default:
		panic("No portage arch defined for " + arch)
	}
	return arch
}
