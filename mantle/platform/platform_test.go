// Copyright 2021 Red Hat, Inc.
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

package platform

import "testing"

func TestSystemUnitFiltering(t *testing.T) {
	// Output from:
	// $ busctl --json=short call \
	//       org.freedesktop.systemd1 \
	//       /org/freedesktop/systemd1 \
	//       org.freedesktop.systemd1.Manager \
	//       ListUnitsFiltered as 2 state failed \
	//       | jq -r '.data[][][0]'
	var output string

	output = `abrt-oops.service
systemd-timesyncd.service`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	if checkSystemdUnitFailures(output, "rhcos") == nil {
		t.Errorf("Should have failed")
	}

	output = `user@1000.service
user-runtime-dir@1000.service`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	if checkSystemdUnitFailures(output, "rhcos") != nil {
		t.Errorf("Should have passed")
	}

	output = `abrt-oops.service
user@1000.service
user-runtime-dir@1000.service`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	if checkSystemdUnitFailures(output, "rhcos") == nil {
		t.Errorf("Should have failed")
	}
}
