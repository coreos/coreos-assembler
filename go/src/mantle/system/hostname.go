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
	"net"
	"os"
	"strings"
)

// FullHostname is a best effort attempt to resolve the canonical FQDN of
// the host. On failure it will fall back to a reasonable looking default
// such as 'localhost.' or 'hostname.invalid.'
func FullHostname() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "localhost" || hostname == "(none)" {
		return "localhost."
	}
	fullname, err := net.LookupCNAME(hostname)
	if err != nil {
		fullname = hostname
		if !strings.Contains(fullname, ".") {
			fullname += ".invalid."
		}
	}
	return fullname
}
