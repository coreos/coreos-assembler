// Copyright 2025 Red Hat, Inc.
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

package fips

import (
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

var fipsConfig = conf.Butane(`
variant: fcos
version: 1.3.0
storage:
  files:
    - path: /etc/ignition-machine-config-encapsulated.json
      mode: 0644
      overwrite: true
      contents:
        inline: |
          {
            "spec": { "fips": true }
          }
`)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "fips.hmac",
		Description: "Verify VM will fail to reboot with FIPS and wrong hmac.",
		Run:         runFipsHMACTest,
		ClusterSize: 1,
		UserData:    fipsConfig,
		Platforms:   []string{"qemu"},
		Tags:        []string{"fips"},
		Distros:     []string{"rhcos"},
		Flags:       []register.Flag{register.NoDracutFatalCheck},
	})
}

func runFipsHMACTest(c cluster.TestCluster) {
	// Run basic FIPS checks
	m := c.Machines()[0]
	c.AssertCmdOutputContains(m, `cat /proc/sys/crypto/fips_enabled`, "1")
	c.AssertCmdOutputContains(m, `update-crypto-policies --show`, "FIPS")

	// Find /boot/ostree/<hash>/.vmlinuz.*.hmac
	hmacFileBytes, err := c.SSH(m, "find /boot/ostree/ -name .vmlinuz-$(uname -r).hmac")
	if err != nil {
		c.Fatalf("Failed to find HMAC file: %v", err)
	}
	hmacFile := strings.TrimSpace(string(hmacFileBytes))
	if hmacFile == "" {
		c.Fatal("HMAC file not found")
	}

	// Remount /boot to change HMAC value in /boot/ostree/<hash>/.vmlinuz.*.hmac
	c.RunCmdSync(m, "sudo mount -o remount,rw /boot")
	c.RunCmdSyncf(m, "sudo sh -c 'echo change > %s'", hmacFile)

	// Initiate reboot
	if err := platform.StartReboot(m); err != nil {
		c.Fatalf("Failed to initiate reboot: %v", err)
	}

	// Wait for the boot to fail. Since the HMAC is corrupted, the machine
	// will fail FIPS integrity check and never come back online.
	// Using a 90 second timeout to allow enough time for boot attempt to fail.
	time.Sleep(90 * time.Second)

	// Verify the machine did not come back online by attempting SSH
	_, _, err = m.SSH("whoami")
	if err == nil {
		c.Fatal("Expected machine to fail booting with corrupted HMAC, but it came back online")
	}

	// Destroy the machine to populate console output
	m.Destroy()

	// Check console output for FIPS integrity failure message
	consoleOutput := m.ConsoleOutput()
	searchPattern := "dracut: FATAL: FIPS integrity test failed"
	if !strings.Contains(consoleOutput, searchPattern) {
		c.Fatalf("Expected to find '%s' in console output after HMAC corruption, but it was not found", searchPattern)
	}
}
