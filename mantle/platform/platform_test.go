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
	// Output from: `systemctl --no-legend --state failed list-units`
	var output string

	output = `● abrt-oops.service         loaded failed failed ABRT kernel log watcher
● systemd-timesyncd.service loaded failed failed Network Time Synchronization`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	output = `abrt-oops.service         loaded failed failed ABRT kernel log watcher
systemd-timesyncd.service loaded failed failed Network Time Synchronization`
	if checkSystemdUnitFailures(output, "rhcos") == nil {
		t.Errorf("Should have failed")
	}

	output = `● user@1000.service             loaded failed failed Foo
● user-runtime-dir@1000.service loaded failed failed Bar`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	output = `user@1000.service             loaded failed failed Foo
user-runtime-dir@1000.service loaded failed failed Bar`
	if checkSystemdUnitFailures(output, "rhcos") != nil {
		t.Errorf("Should have passed")
	}

	output = `● abrt-oops.service         loaded failed failed ABRT kernel log watcher
● user@1000.service             loaded failed failed Foo
● user-runtime-dir@1000.service loaded failed failed Bar`
	if checkSystemdUnitFailures(output, "fcos") == nil {
		t.Errorf("Should have failed")
	}
	output = `abrt-oops.service         loaded failed failed ABRT kernel log watcher
user@1000.service             loaded failed failed Foo
user-runtime-dir@1000.service loaded failed failed Bar`
	if checkSystemdUnitFailures(output, "rhcos") == nil {
		t.Errorf("Should have failed")
	}
}
