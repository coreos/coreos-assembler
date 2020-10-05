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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

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
)

var signalCompletionUnit = `[Unit]
Requires=dev-virtio\\x2dports-completion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
ExecStart=/bin/sh -c '/usr/bin/echo coreos-installer-test-OK >/dev/virtio-ports/completion && systemctl poweroff'
[Install]
RequiredBy=multi-user.target
`

func init() {
	cmdTestIso.Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	cmdTestIso.Flags().BoolVarP(&legacy, "legacy", "K", false, "Test legacy installer")
	cmdTestIso.Flags().BoolVarP(&nolive, "no-live", "L", false, "Skip testing live installer")

	root.AddCommand(cmdTestIso)
}

func runTestIso(cmd *cobra.Command, args []string) error {
	// SKIP testio due issues in POWER. Check issue #1757
	if runtime.GOARCH == "ppc64le" {
		fmt.Println("The testiso is disabled for ppc64le")
		return nil
	}
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --cosa-build")
	}

	baseInst := platform.Install{
		CosaBuildDir: kola.Options.CosaBuild,
		CosaBuild:    kola.CosaBuild,

		Board:    kola.QEMUOptions.Board,
		Firmware: kola.QEMUOptions.Firmware,
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

	baseInst.QemuArgs = []string{
		"-device", "virtio-serial", "-device", "virtserialport,chardev=completion,name=completion",
		"-chardev", "file,id=completion,path=" + completionfile}

	if kola.CosaBuild.BuildArtifacts.Metal == nil {
		return fmt.Errorf("Build %s must have a `metal` artifact", kola.CosaBuild.OstreeVersion)
	}

	ranTest := false

	foundLegacy := baseInst.CosaBuild.BuildArtifacts.Kernel != nil
	if foundLegacy {
		if legacy {
			ranTest = true
			inst := baseInst // Pretend this is Rust and I wrote .copy()
			inst.LegacyInstaller = true

			if err := testPXE(inst, completionfile); err != nil {
				return err
			}
			fmt.Printf("Successfully tested legacy installer for %s\n", kola.CosaBuild.OstreeVersion)
		}
	} else if legacy {
		return fmt.Errorf("build %s has no legacy installer kernel", kola.CosaBuild.Name)
	}

	foundLive := kola.CosaBuild.BuildArtifacts.LiveKernel != nil
	if !nolive {
		if !foundLive {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Name)
		}
		ranTest = true
		inst := baseInst // Pretend this is Rust and I wrote .copy()

		if err := testPXE(inst, completionfile); err != nil {
			return err
		}

		fmt.Printf("Successfully tested live installer for %s\n", kola.CosaBuild.OstreeVersion)
	}

	if !ranTest {
		return fmt.Errorf("Nothing to test!")
	}

	return nil
}

func testPXE(inst platform.Install, completionfile string) error {
	completionstamp := "coreos-installer-test-OK"

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
	if sdk.TargetIgnitionVersion(kola.CosaBuild) == "v2" {
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

	mach, err := inst.PXE(nil, configStr)
	if err != nil {
		return errors.Wrapf(err, "running PXE")
	}
	defer mach.Destroy()

	err = mach.QemuInst.Wait()
	if err != nil {
		return err
	}

	err = exec.Command("grep", "-q", "-e", completionstamp, completionfile).Run()
	if err != nil {
		return fmt.Errorf("Failed to find %s in %s: %s", completionstamp, completionfile, err)
	}

	err = os.Remove(completionfile)
	if err != nil {
		return errors.Wrapf(err, "removing %s", completionfile)
	}

	return nil
}
