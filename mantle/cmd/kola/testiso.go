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

// TODO:
// - Support testing the "just run Live" case - maybe try to figure out
//   how to have main `kola` tests apply?
// - Test `coreos-install iso embed` path

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/sdk"

	ignconverter "github.com/coreos/ign-converter"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
)

var (
	cmdTestIso = &cobra.Command{
		RunE:    runTestIso,
		PreRunE: preRun,
		Use:     "testiso",
		Short:   "Test a CoreOS PXE boot or ISO install path",

		SilenceUsage: true,
	}

	instInsecure bool

	legacy bool
	nolive bool
	nopxe  bool
	noiso  bool

	console bool
)

var signalCompleteString = "coreos-installer-test-OK"
var signalCompletionUnit = fmt.Sprintf(`[Unit]
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=multi-user.target
`, signalCompleteString)

func init() {
	cmdTestIso.Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	cmdTestIso.Flags().BoolVarP(&legacy, "legacy", "K", false, "Test legacy installer")
	cmdTestIso.Flags().BoolVarP(&nolive, "no-live", "L", false, "Skip testing live installer (PXE and ISO)")
	cmdTestIso.Flags().BoolVarP(&nopxe, "no-pxe", "P", false, "Skip testing live installer PXE")
	cmdTestIso.Flags().BoolVarP(&noiso, "no-iso", "", false, "Skip testing live installer ISO")
	cmdTestIso.Flags().BoolVar(&console, "console", false, "Display qemu console to stdout")

	root.AddCommand(cmdTestIso)
}

func newQemuBuilder() *platform.QemuBuilder {
	builder := platform.NewMetalQemuBuilderDefault()
	builder.Firmware = kola.QEMUOptions.Firmware
	sectorSize := 0
	if kola.QEMUOptions.Native4k {
		sectorSize = 4096
	}
	builder.AddPrimaryDisk(&platform.Disk{
		Size: "12G", // Arbitrary

		SectorSize: sectorSize,
	})

	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	builder.Memory = 4096
	if system.RpmArch() == "s390x" {
		// FIXME - determine why this is
		builder.Memory = int(math.Max(float64(builder.Memory), 16384))
	}

	builder.InheritConsole = console

	return builder
}

func runTestIso(cmd *cobra.Command, args []string) error {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --cosa-build")
	}

	baseInst := platform.Install{
		CosaBuild: kola.CosaBuild,
		Native4k:  kola.QEMUOptions.Native4k,
	}

	if instInsecure {
		baseInst.Insecure = true
	}

	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	completionfile := filepath.Join(tmpd, "completion.txt")

	if kola.CosaBuild.Meta.BuildArtifacts.Metal == nil {
		return fmt.Errorf("Build %s must have a `metal` artifact", kola.CosaBuild.Meta.OstreeVersion)
	}

	ranTest := false

	foundLegacy := baseInst.CosaBuild.Meta.BuildArtifacts.Kernel != nil
	if foundLegacy {
		if legacy {
			ranTest = true
			inst := baseInst // Pretend this is Rust and I wrote .copy()
			inst.LegacyInstaller = true

			if err := testPXE(inst); err != nil {
				return err
			}
			fmt.Printf("Successfully tested legacy installer for %s\n", kola.CosaBuild.Meta.OstreeVersion)
		}
	} else if legacy {
		return fmt.Errorf("build %s has no legacy installer kernel", kola.CosaBuild.Meta.Name)
	}

	foundLive := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel != nil
	if !nolive {
		if !foundLive {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Meta.Name)
		}
		if !nopxe {
			ranTest = true
			instPxe := baseInst // Pretend this is Rust and I wrote .copy()

			if err := testPXE(instPxe); err != nil {
				return err
			}
			printSuccess("PXE")
		}

		if !noiso {
			ranTest = true
			instIso := baseInst // Pretend this is Rust and I wrote .copy()
			if err := testLiveIso(instIso, completionfile); err != nil {
				return err
			}
			printSuccess("ISO")
		}
	}

	if !ranTest {
		return fmt.Errorf("Nothing to test!")
	}

	return nil
}

func awaitCompletion(inst *platform.QemuInstance, qchan *os.File, expected []string) error {
	errchan := make(chan error)
	go func() {
		if err := inst.WaitAll(); err != nil {
			errchan <- err
		}
		time.Sleep(1 * time.Minute)
		errchan <- fmt.Errorf("QEMU exited; timed out waiting for completion")
	}()
	go func() {
		r := bufio.NewReader(qchan)
		for _, exp := range expected {
			l, err := r.ReadString('\n')
			if err != nil {
				errchan <- errors.Wrapf(err, "reading from completion channel")
				return
			}
			line := strings.TrimSpace(l)
			if line != exp {
				errchan <- fmt.Errorf("Unexpected string from completion channel: %s expected: %s", line, exp)
				return
			}
		}
		// OK!
		errchan <- nil
	}()
	return <-errchan
}

func printSuccess(mode string) {
	metaltype := "metal"
	if kola.QEMUOptions.Native4k {
		metaltype = "metal4k"
	}
	fmt.Printf("Successfully tested %s live installer for %s on %s (%s)\n", mode, kola.CosaBuild.Meta.OstreeVersion, kola.QEMUOptions.Firmware, metaltype)
}

func testPXE(inst platform.Install) error {
	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "coreos-test-installer.service",
					Contents: &signalCompletionUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
	var configStr string
	if sdk.TargetIgnitionVersion(kola.CosaBuild.Meta) == "v2" {
		ignc2, err := ignconverter.Translate3to2(config)
		if err != nil {
			return err
		}
		buf, err := json.Marshal(ignc2)
		if err != nil {
			return err
		}
		configStr = string(buf)
	} else {
		buf, err := json.Marshal(config)
		if err != nil {
			return err
		}
		configStr = string(buf)
	}

	inst.Builder = newQemuBuilder()
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	mach, err := inst.PXE(nil, configStr)
	if err != nil {
		return errors.Wrapf(err, "running PXE")
	}
	defer mach.Destroy()

	return awaitCompletion(mach.QemuInst, completionChannel, []string{signalCompleteString})
}

func testLiveIso(inst platform.Install, completionfile string) error {
	inst.Builder = newQemuBuilder()
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	// We're just testing that executing our custom Ignition in the live
	// path worked ok.
	liveOKSignal := "live-test-OK"
	var liveSignalOKUnit = fmt.Sprintf(`[Unit]
	Requires=dev-virtio\\x2dports-testisocompletion.device
	OnFailure=emergency.target
	OnFailureJobMode=isolate
	Before=coreos-installer.service
	[Service]
	Type=oneshot
	RemainAfterExit=yes
	ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
	[Install]
	RequiredBy=multi-user.target
	`, liveOKSignal)

	liveConfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "live-signal-ok.service",
					Contents: &liveSignalOKUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
	liveConfigBuf, err := json.Marshal(liveConfig)
	if err != nil {
		return err
	}

	targetConfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     "coreos-test-installer.service",
					Contents: &signalCompletionUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}

	targetIgnitionBuf, err := json.Marshal(targetConfig)
	if err != nil {
		return err
	}
	mach, err := inst.InstallViaISOEmbed(nil, string(liveConfigBuf), string(targetIgnitionBuf))
	if err != nil {
		return errors.Wrapf(err, "running iso install")
	}
	defer mach.Destroy()

	return awaitCompletion(mach.QemuInst, completionChannel, []string{liveOKSignal, signalCompleteString})
}
