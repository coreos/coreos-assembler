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

package conf

import (
	"net"
	"strings"
	"testing"

	"github.com/coreos/mantle/network"
)

func TestConfCopyKey(t *testing.T) {
	agent, err := network.NewSSHAgent(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewSSHAgent failed: %v", err)
	}

	keys, err := agent.List()
	if err != nil {
		t.Fatalf("agent.List failed: %v", err)
	}

	tests := []*UserData{
		Butane(""),
		Ignition(`{ "ignitionVersion": 1 }`),
		Ignition(`{ "ignition": { "version": "2.0.0" } }`),
		Ignition(`{ "ignition": { "version": "2.1.0" } }`),
		Ignition(`{ "ignition": { "version": "2.2.0" } }`),
		Ignition(`{ "ignition": { "version": "2.3.0" } }`),
		Ignition(`{ "ignition": { "version": "2.4.0" } }`),
		Ignition(`{ "ignition": { "version": "3.5.0" } }`),
	}

	for _, tt := range tests {
		_, err := tt.Render(FailWarnings)
		if err == nil {
			t.Errorf("parsed an unsupported config!")
			continue
		}
	}

	tests = []*UserData{
		Butane("variant: fcos\nversion: 1.3.0"),
		Ignition(`{ "ignition": { "version": "3.0.0" } }`),
		Ignition(`{ "ignition": { "version": "3.1.0" } }`),
		Ignition(`{ "ignition": { "version": "3.2.0" } }`),
		Ignition(`{ "ignition": { "version": "3.3.0" } }`),
		Ignition(`{ "ignition": { "version": "3.4.0-experimental" } }`),
		// special-case handling of next stable spec
		Ignition(`{ "ignition": { "version": "3.4.0" } }`),
	}

	for i, tt := range tests {
		conf, err := tt.Render(FailWarnings)
		if err != nil {
			t.Errorf("failed to parse config %d: %v", i, err)
			continue
		}

		conf.CopyKeys(keys)

		str := conf.String()

		if !strings.Contains(str, "ecdsa-sha2-nistp256 ") || !strings.Contains(str, " core@default") {
			t.Errorf("ssh public key not found in config %d: %s", i, str)
			continue
		}
	}
}
