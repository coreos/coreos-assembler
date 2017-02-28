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

package omaha

import (
	"runtime"
)

// Translate GOARCH to Omaha's choice of names, because no two independent
// software projects *ever* use the same set of architecture names. ;-)
func LocalArch() string {
	switch runtime.GOARCH {
	case "386":
		return "x86"
	case "amd64":
		return "x64"
	case "amd64p32":
		// Not actually specified by Omaha but it follows the above.
		return "x32"
	case "arm":
		fallthrough
	default:
		// Nothing else is defined by Omaha so anything goes.
		return runtime.GOARCH
	}
}

// Translate GOOS to Omaha's platform names as best as we can.
func LocalPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac" // or "ios"
	case "linux":
		return "linux" // or "android"
	case "windows":
		return "win"
	default:
		// Nothing else is defined by Omaha so anything goes.
		return runtime.GOOS
	}
}
