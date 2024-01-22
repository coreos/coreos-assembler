// Copyright 2020 Red Hat, Inc.
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

package ignition

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
	"github.com/coreos/ignition/v2/config/v3_2/types"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.failure",
		Description: "Verify ignition will fail with unsupported action.",
		Run:         runIgnitionFailure,
		ClusterSize: 0,
		Platforms:   []string{"qemu"},
		Tags:        []string{"ignition"},
	})
	register.RegisterTest(&register.Test{
		Name:        "coreos.unique.boot.failure",
		ClusterSize: 0,
		Description: "Verify boot fails if there are pre-existing boot filesystems.",
		Platforms:   []string{"qemu"},
		Run:         runDualBootfsFailure,
	})
	register.RegisterTest(&register.Test{
		Name:        "coreos.unique.boot.ignition.failure",
		ClusterSize: 0,
		Description: "Verify boot fails if there are pre-existing boot filesystems created with Ignition.",
		Platforms:   []string{"qemu"},
		Run:         runDualBootfsIgnitionFailure,
	})
}

func runIgnitionFailure(c cluster.TestCluster) {
	if err := ignitionFailure(c); err != nil {
		c.Fatal(err.Error())
	}
}

func runDualBootfsFailure(c cluster.TestCluster) {
	if err := dualBootfsFailure(c); err != nil {
		c.Fatal(err.Error())
	}
}

func runDualBootfsIgnitionFailure(c cluster.TestCluster) {
	if err := dualBootfsIgnitionFailure(c); err != nil {
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
	// We can't create files in / due to the immutable bit OSTree creates, so
	// this is a convenient way to test Ignition failure.
	failConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return errors.Wrapf(err, "creating empty config")
	}
	failConfig.AddFile("/notwritable.txt", "Hello world", 0644)

	builder := platform.NewQemuBuilder()
	defer builder.Close()
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

	builder.MemoryMiB = 1024
	builder.Firmware = kola.QEMUOptions.Firmware

	searchPattern := "error creating /sysroot/notwritable.txt"
	if err := verifyError(builder, searchPattern); err != nil {
		return err
	}
	return nil
}

// Verify that there is only one boot filesystem attached to the device
func dualBootfsFailure(c cluster.TestCluster) error {
	builder := platform.NewQemuBuilder()
	defer builder.Close()
	// Create a temporary log file allocated in the output dir of the test
	consoleFile := c.H.TempFile("console-")
	// Instruct builder to use it
	builder.ConsoleFile = consoleFile.Name()
	// get current path and create tmp dir
	fakeBootFile, err := builder.TempFile("fakeBoot")
	if err != nil {
		return err
	}

	// Truncate the file to 1 gigabyte
	const oneGB = 1 << 30
	err = fakeBootFile.Truncate(oneGB)
	if err != nil {
		return err
	}

	cmd := exec.Command("mkfs.ext4", "-L", "boot", fakeBootFile.Name())
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		c.Fatal(err)
	}

	err = builder.AddBootDisk(&platform.Disk{
		BackingFile: kola.QEMUOptions.DiskImage,
	})
	if err != nil {
		return err
	}
	err = builder.AddDisk(&platform.Disk{
		BackingFile:   fakeBootFile.Name(),
		BackingFormat: "raw",
	})
	if err != nil {
		return err
	}
	builder.MemoryMiB = 1024
	builder.Firmware = kola.QEMUOptions.Firmware

	searchRegexString := "Error: System has 2 devices with a filesystem labeled 'boot'"
	if err := verifyError(builder, searchRegexString); err != nil {
		return err
	}
	return nil
}

// Use ignition config to create a second bootfs
// 1 - produce an ignition file that format a disk with a"boot" label.
// 2 - boot the VM with the ignition file and an extra disk.
// 3 - observe the failure
func dualBootfsIgnitionFailure(c cluster.TestCluster) error {
	builder := platform.NewQemuBuilder()
	defer builder.Close()
	// Create a temporary log file
	consoleFile := c.H.TempFile("console-")
	// Instruct builder to use it
	builder.ConsoleFile = consoleFile.Name()
	failConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return errors.Wrapf(err, "creating empty config")
	}

	// Craft an Ignition file that formats a partition
	formaterConfig := types.Config{
		Ignition: types.Ignition{
			Version: "3.2.0",
		},
		Storage: types.Storage{
			Filesystems: []types.Filesystem{
				{
					Device:         "/dev/disk/by-id/virtio-extra-boot",
					Label:          util.StrToPtr("boot"),
					Format:         util.StrToPtr("vfat"),
					WipeFilesystem: util.BoolToPtr(true),
				},
			},
		},
	}
	failConfig.MergeV32(formaterConfig)

	builder.SetConfig(failConfig)
	err = builder.AddBootDisk(&platform.Disk{
		BackingFile: kola.QEMUOptions.DiskImage,
	})

	if err != nil {
		return err
	}

	err = builder.AddDisksFromSpecs([]string{"1G:serial=extra-boot"})
	if err != nil {
		return err
	}

	builder.MemoryMiB = 1024
	builder.Firmware = kola.QEMUOptions.Firmware

	searchRegexString := "Error: System has 2 devices with a filesystem labeled 'boot'"
	if err := verifyError(builder, searchRegexString); err != nil {
		return err
	}
	return nil
}
