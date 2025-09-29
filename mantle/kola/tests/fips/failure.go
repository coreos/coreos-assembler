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
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/pkg/errors"

	coreosarch "github.com/coreos/stream-metadata-go/arch"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

var failConfig = conf.Ignition(`{
	"ignition": {
		"version": "3.4.0"
	},
	"storage": {
		"filesystems": [
			{
				"device": "/dev/mapper/root",
				"format": "xfs",
				"label": "root",
				"wipeFilesystem": true
			}
		],
		"luks": [
			{
				"clevis": {
					"tpm2": true
				},
				"device": "/dev/disk/by-partlabel/root",
				"label": "luks-root",
				"name": "root",
				"options": [
					"--cipher",
					"aes-cbc-essiv:sha256",
					"--pbkdf",
					"argon2i"
				],
				"wipeVolume": true
			}
		],
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
		Name:        "fips.failure",
		Description: "Verify cryptsetup lukscreate will fail with FIPS and incompatible crypto algorithms.",
		Run:         runFipsFailure,
		ClusterSize: 0,
		Platforms:   []string{"qemu"},
		Tags:        []string{"ignition"},
		Distros:     []string{"rhcos", "scos"},
	})
}

func runFipsFailure(c cluster.TestCluster) {
	if err := ignitionFailure(c); err != nil {
		c.Fatal(err.Error())
	}
}

// Read file and verify if it contains a pattern
// 1. Read file, make sure it exists
// 2. regex for pattern
func fileContainsPattern(path string, searchPattern string) (bool, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	// File has content, but the pattern is not present
	match := regexp.MustCompile(searchPattern).Match(file)
	if match {
		// Pattern found
		return true, nil
	}
	// Pattern not found
	return false, nil
}

// Start the VM, take string and grep for it in the temporary console logs
func verifyError(builder *platform.QemuBuilder, searchPattern string) error {
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)

	defer cancel()

	errchan := make(chan error)
	go func() {
		resultingError := inst.WaitAll(ctx)
		if resultingError == nil {
			resultingError = fmt.Errorf("ignition unexpectedly succeeded")
		} else if resultingError == platform.ErrInitramfsEmergency {
			// Expected initramfs failure, checking the console file to ensure
			// that it failed the expected way
			found, err := fileContainsPattern(builder.ConsoleFile, searchPattern)
			if err != nil {
				resultingError = errors.Wrapf(err, "looking for pattern '%s' in file '%s' failed", searchPattern, builder.ConsoleFile)
			} else if !found {
				resultingError = fmt.Errorf("pattern '%s' in file '%s' not found", searchPattern, builder.ConsoleFile)
			} else {
				// The expected case
				resultingError = nil
			}
		} else {
			resultingError = errors.Wrapf(resultingError, "expected initramfs emergency.target error")
		}
		errchan <- resultingError
	}()

	select {
	case <-ctx.Done():
		if err := inst.Kill(); err != nil {
			return errors.Wrapf(err, "failed to kill the vm instance")
		}
		return errors.Wrapf(ctx.Err(), "timed out waiting for initramfs error")
	case err := <-errchan:
		if err != nil {
			return err
		}
		return nil
	}
}

func ignitionFailure(c cluster.TestCluster) error {
	builder := platform.NewQemuBuilder()
	defer builder.Close()

	// Prepare Ingnition config
	failConfig, err := failConfig.Render(conf.FailWarnings)
	if err != nil {
		return errors.Wrapf(err, "creating invalid FIPS config")
	}

	// Create a temporary log file
	consoleFile := c.H.TempFile("console-")

	// Instruct builder to use it
	builder.ConsoleFile = consoleFile.Name()
	builder.SetConfig(failConfig)
	err = builder.AddBootDisk(&platform.Disk{
		BackingFile: kola.QEMUOptions.DiskImage,
	})
	if err != nil {
		return err
	}

	builder.MemoryMiB = 4096
	switch coreosarch.CurrentRpmArch() {
	case "ppc64le":
		builder.MemoryMiB = 8192
	}
	builder.Firmware = kola.QEMUOptions.Firmware

	searchPattern := "Only PBKDF2 is supported in FIPS mode"
	if err := verifyError(builder, searchPattern); err != nil {
		return err
	}
	return nil
}
