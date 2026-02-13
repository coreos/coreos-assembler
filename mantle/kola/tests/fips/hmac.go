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

var fipsConfig = conf.Ignition(`{
	"ignition": {
		"version": "3.4.0"
	},
	"storage": {
		"files": [
			{
				"group": {
					"name": "root"
				},
				"overwrite": true,
				"path": "/etc/ignition-machine-config-encapsulated.json",
				"user": {
					"name": "root"
				},
				"contents": {
					"source": "data:,%7B%22metadata%22%3A%7B%22name%22%3A%22rendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22selfLink%22%3A%22%2Fapis%2Fmachineconfiguration.openshift.io%2Fv1%2Fmachineconfigs%2Frendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22uid%22%3A%2248871c03-899d-4332-a5f5-bef94e54b23f%22%2C%22resourceVersion%22%3A%224168%22%2C%22generation%22%3A1%2C%22creationTimestamp%22%3A%222019-11-04T15%3A54%3A08Z%22%2C%22annotations%22%3A%7B%22machineconfiguration.openshift.io%2Fgenerated-by-controller-version%22%3A%22bd846958bc95d049547164046a962054fca093df%22%7D%2C%22ownerReferences%22%3A%5B%7B%22apiVersion%22%3A%22machineconfiguration.openshift.io%2Fv1%22%2C%22kind%22%3A%22MachineConfigPool%22%2C%22name%22%3A%22worker%22%2C%22uid%22%3A%223d0dee9e-c9d6-4656-a4a9-81785b9ab01a%22%2C%22controller%22%3Atrue%2C%22blockOwnerDeletion%22%3Atrue%7D%5D%7D%2C%22spec%22%3A%7B%22osImageURL%22%3A%22registry.svc.ci.openshift.org%2Focp%2F4.3-2019-11-04-125204%40sha256%3A8a344c5b157bd01c3ca1abfcef0004fc39f5d69cac1cdaad0fd8dd332ad8e272%22%2C%22config%22%3A%7B%22ignition%22%3A%7B%22config%22%3A%7B%7D%2C%22security%22%3A%7B%22tls%22%3A%7B%7D%7D%2C%22timeouts%22%3A%7B%7D%2C%22version%22%3A%223.0.0%22%7D%2C%22networkd%22%3A%7B%7D%2C%22passwd%22%3A%7B%7D%2C%22storage%22%3A%7B%7D%2C%22systemd%22%3A%7B%7D%7D%2C%22kernelArguments%22%3A%5B%5D%2C%22fips%22%3Atrue%7D%7D",
					"verification": {}
				},
				"mode": 420
			}
		]
	}
}`)

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
	_, _, err = m.SSH("echo test")
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
