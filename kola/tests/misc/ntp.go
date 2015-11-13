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

package misc

import (
	"bytes"
	"fmt"
	"time"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

func init() {
	register.Register(&register.Test{
		Run:         NTP,
		ClusterSize: 0,
		Name:        "linux.ntp",
		Platforms:   []string{"qemu"},
	})
}

// Test that timesyncd starts using the local NTP server
func NTP(c platform.TestCluster) error {
	m, err := c.NewMachine("")
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine: %s", err)
	}
	defer m.Destroy()

	out, err := m.SSH("networkctl status eth0")
	if err != nil {
		return fmt.Errorf("networkctl: %v", err)
	}
	if !bytes.Contains(out, []byte("NTP: 10.0.0.1")) {
		return fmt.Errorf("Bad network config:\n%s", out)
	}

	plog.Info("Waiting for systemd-timesyncd.service")

	checker := func() error {
		out, err = m.SSH("systemctl status systemd-timesyncd.service")
		if err != nil {
			return fmt.Errorf("systemctl: %v", err)
		}

		if !bytes.Contains(out, []byte(`Status: "Synchronized to time server 10.0.0.1:123 (10.0.0.1)."`)) {
			return fmt.Errorf("unexpected systemd-timesyncd status: %v", out)
		}

		plog.Info("systemd-timesyncd.service is working!")
		return nil
	}

	err = util.Retry(60, 1*time.Second, checker)
	if err != nil {
		return nil
	}

	return nil
}
