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

package platform

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

	tests := []struct {
		conf string
	}{
		{`{ "ignitionVersion": 1 }`},
		{"#cloud-config"},
	}

	for i, tt := range tests {
		conf, err := NewConf(tt.conf)
		if err != nil {
			t.Errorf("failed to parse config %d: %v", i, err)
			continue
		}

		conf.CopyKeys(keys)

		str := conf.String()

		if !strings.Contains(str, "ssh-rsa ") || !strings.Contains(str, " core@default") {
			t.Errorf("ssh public key not found in config %d: %s", i, str)
			continue
		}
	}
}
