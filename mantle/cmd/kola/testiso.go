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
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	ignv3 "github.com/coreos/ignition/v2/config/v3_0"
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

	scenarios []string

	pxeKernelArgs []string

	debug bool
)

const (
	installTimeout = 10 * time.Minute

	scenarioPXEInstall = "pxe-install"
	scenarioISOInstall = "iso-install"

	scenarioISOOfflineInstall = "iso-offline-install"
	scenarioISOLiveLogin      = "iso-live-login"
	scenarioLegacyInstall     = "legacy-install"
)

var allScenarios = map[string]bool{
	scenarioPXEInstall:        true,
	scenarioISOInstall:        true,
	scenarioISOOfflineInstall: true,
	scenarioISOLiveLogin:      true,
	scenarioLegacyInstall:     true,
}

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
	// TODO remove these --no-X args once RHCOS switches to the live ISO
	cmdTestIso.Flags().BoolVarP(&nolive, "no-live", "L", false, "Skip testing live installer (PXE and ISO)")
	cmdTestIso.Flags().BoolVarP(&nopxe, "no-pxe", "P", false, "Skip testing live installer PXE")
	cmdTestIso.Flags().BoolVarP(&noiso, "no-iso", "", false, "Skip testing live installer ISO")
	cmdTestIso.Flags().BoolVar(&debug, "debug", false, "Display qemu console to stdout, turn off automatic initramfs failure checking")
	cmdTestIso.Flags().StringSliceVar(&pxeKernelArgs, "pxe-kargs", nil, "Additional kernel arguments for PXE")
	// FIXME move scenarioISOLiveLogin into the defaults once https://github.com/coreos/fedora-coreos-config/pull/339#issuecomment-613000050 is fixed
	cmdTestIso.Flags().StringSliceVar(&scenarios, "scenarios", []string{scenarioPXEInstall, scenarioISOOfflineInstall}, fmt.Sprintf("Test scenarios (also available: %v)", []string{scenarioLegacyInstall, scenarioISOLiveLogin, scenarioISOInstall}))
	cmdTestIso.Args = cobra.ExactArgs(0)

	root.AddCommand(cmdTestIso)
}

func newBaseQemuBuilder() *platform.QemuBuilder {
	builder := platform.NewMetalQemuBuilderDefault()
	builder.Firmware = kola.QEMUOptions.Firmware

	// https://github.com/coreos/fedora-coreos-tracker/issues/388
	// https://github.com/coreos/fedora-coreos-docs/pull/46
	builder.Memory = 4096
	if system.RpmArch() == "s390x" {
		// After some trial and error looks like we need at least 10G on s390x
		// Recorded an issue to investigate this: https://github.com/coreos/coreos-assembler/issues/1489
		builder.Memory = int(math.Max(float64(builder.Memory), 10240))
	}

	builder.InheritConsole = debug

	return builder
}

func newQemuBuilder(isPXE bool, outdir string) (*platform.QemuBuilder, *ignv3types.Config, error) {
	builder := newBaseQemuBuilder()
	sectorSize := 0
	if kola.QEMUOptions.Native4k {
		sectorSize = 4096
	}

	if system.RpmArch() == "s390x" && isPXE {
		// For s390x PXE installs the network device has the bootindex of 1.
		// Do not use a primary disk in case of net-booting for this test
		builder.AddDisk(&platform.Disk{
			Size: "12G", // Arbitrary

			SectorSize: sectorSize,
		})
	} else {
		builder.AddPrimaryDisk(&platform.Disk{
			Size: "12G", // Arbitrary

			SectorSize: sectorSize,
		})
	}
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return nil, nil, err
	}

	if !builder.InheritConsole {
		builder.ConsoleToFile(filepath.Join(outdir, "console.txt"))
	}
	virtioJournalConfig, journalPipe, err := builder.VirtioJournal("")
	if err != nil {
		return nil, nil, err
	}
	journalOut, err := os.OpenFile(filepath.Join(outdir, "journal.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, nil, err
	}

	go func() {
		_, err := io.Copy(journalOut, journalPipe)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	return builder, virtioJournalConfig, nil
}

func runTestIso(cmd *cobra.Command, args []string) error {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --build")
	}

	targetScenarios := make(map[string]bool)
	for _, scenario := range scenarios {
		if _, ok := allScenarios[scenario]; !ok {
			return fmt.Errorf("Unknown scenario: %s", scenario)
		}
		targetScenarios[scenario] = true
	}

	// ppc64le: pxe-install does not work: https://github.com/coreos/coreos-assembler/issues/1457. Seems like
	// the SLOF doesn't like the live initramfs image.
	// s390x: pxe-install does not work because the bootimage used today is built from the rhcos kernel+initrd
	// since s390x does not have a pre-built all in one image. For the pxe case, this image turns out to
	// be bigger than the allowed tftp buffer. iso-install does not work because s390x uses an El Torito image
	switch system.RpmArch() {
	case "s390x":
		fmt.Println("Skipping pxe-install and iso-install on s390x")
		noiso = true
		nopxe = true
	case "ppc64le":
		fmt.Println("Skipping pxe-install on ppc64le")
		nopxe = true
	}

	if legacy {
		targetScenarios[scenarioLegacyInstall] = true
	}
	if nopxe || nolive {
		delete(targetScenarios, scenarioPXEInstall)
	}
	if noiso || nolive {
		delete(targetScenarios, scenarioISOInstall)
		delete(targetScenarios, scenarioISOOfflineInstall)
		delete(targetScenarios, scenarioISOLiveLogin)
	}

	// just make it a normal print message so pipelines don't error out for ppc64le and s390x
	if len(targetScenarios) == 0 {
		fmt.Println("No valid scenarios specified!")
		return nil
	}
	scenarios = []string{}
	for scenario, _ := range targetScenarios {
		scenarios = append(scenarios, scenario)
	}
	fmt.Printf("Testing scenarios: %s\n", scenarios)

	var err error
	// note this reassigns a *global*
	outputDir, err = kola.SetupOutputDir(outputDir, "testiso")
	if err != nil {
		return err
	}

	baseInst := platform.Install{
		CosaBuild: kola.CosaBuild,
		Native4k:  kola.QEMUOptions.Native4k,

		IgnitionSpec2: kola.Options.IgnitionVersion == "v2",
	}

	if instInsecure {
		baseInst.Insecure = true
	}

	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	ranTest := false

	foundLegacy := baseInst.CosaBuild.Meta.BuildArtifacts.Kernel != nil
	if foundLegacy {
		if _, ok := targetScenarios[scenarioLegacyInstall]; ok {
			ranTest = true
			inst := baseInst // Pretend this is Rust and I wrote .copy()
			inst.LegacyInstaller = true

			if err := testPXE(inst, filepath.Join(outputDir, scenarioLegacyInstall)); err != nil {
				return errors.Wrapf(err, "scenario %s", scenarioLegacyInstall)
			}
			printSuccess(scenarioLegacyInstall)
		}
	} else if _, ok := targetScenarios[scenarioLegacyInstall]; ok {
		return fmt.Errorf("build %s has no legacy installer kernel", kola.CosaBuild.Meta.Name)
	}

	foundLive := kola.CosaBuild.Meta.BuildArtifacts.LiveKernel != nil
	if _, ok := targetScenarios[scenarioPXEInstall]; ok {
		if !foundLive {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Meta.Name)
		}

		ranTest = true
		instPxe := baseInst // Pretend this is Rust and I wrote .copy()

		if err := testPXE(instPxe, filepath.Join(outputDir, scenarioPXEInstall)); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioPXEInstall)

		}
		printSuccess(scenarioPXEInstall)
	}
	if _, ok := targetScenarios[scenarioISOInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		instIso := baseInst // Pretend this is Rust and I wrote .copy()
		if err := testLiveIso(instIso, filepath.Join(outputDir, scenarioISOInstall), false); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioISOInstall)
		}
		printSuccess(scenarioISOInstall)
	}
	if _, ok := targetScenarios[scenarioISOOfflineInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		instIso := baseInst // Pretend this is Rust and I wrote .copy()
		if err := testLiveIso(instIso, filepath.Join(outputDir, scenarioISOOfflineInstall), true); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioISOOfflineInstall)
		}
		printSuccess(scenarioISOOfflineInstall)
	}
	if _, ok := targetScenarios[scenarioISOLiveLogin]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		if err := testLiveLogin(filepath.Join(outputDir, scenarioISOLiveLogin)); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioISOLiveLogin)
		}
		printSuccess(scenarioISOLiveLogin)
	}

	if !ranTest {
		panic("Nothing was tested!")
	}

	return nil
}

func awaitCompletion(inst *platform.QemuInstance, outdir string, qchan *os.File, expected []string) error {
	errchan := make(chan error)
	go func() {
		time.Sleep(installTimeout)
		errchan <- fmt.Errorf("timed out after %v", installTimeout)
	}()
	if !debug {
		go func() {
			errBuf, err := inst.WaitIgnitionError()
			if err == nil {
				if errBuf != "" {
					msg := fmt.Sprintf("entered emergency.target in initramfs")
					plog.Info(msg)
					path := filepath.Join(outdir, "ignition-virtio-dump.txt")
					if err := ioutil.WriteFile(path, []byte(errBuf), 0644); err != nil {
						plog.Errorf("Failed to write journal: %v", err)
					}
					err = platform.ErrInitramfsEmergency
				}
			}
			if err != nil {
				errchan <- err
			}
		}()
	}
	go func() {
		var err error
		err = inst.Wait()
		if err != nil {
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
				if err == io.EOF {
					errchan <- fmt.Errorf("Got EOF from completion channel, %s expected", exp)
				} else {
					errchan <- errors.Wrapf(err, "reading from completion channel")
				}
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
	fmt.Printf("Successfully tested scenario %s for %s on %s (%s)\n", mode, kola.CosaBuild.Meta.OstreeVersion, kola.QEMUOptions.Firmware, metaltype)
}

func testPXE(inst platform.Install, outdir string) error {
	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)
	sshPubKey := ignv3types.SSHAuthorizedKey(strings.TrimSpace(string(sshPubKeyBuf)))
	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Passwd: ignv3types.Passwd{
			Users: []ignv3types.PasswdUser{
				{
					Name: "core",
					SSHAuthorizedKeys: []ignv3types.SSHAuthorizedKey{
						sshPubKey,
					},
				},
			},
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

	builder, virtioJournalConfig, err := newQemuBuilder(true, outdir)
	if err != nil {
		return err
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	config = ignv3.Merge(config, *virtioJournalConfig)

	mach, err := inst.PXE(pxeKernelArgs, config)
	if err != nil {
		return errors.Wrapf(err, "running PXE")
	}
	defer mach.Destroy()

	return awaitCompletion(mach.QemuInst, outdir, completionChannel, []string{signalCompleteString})
}

func testLiveIso(inst platform.Install, outdir string, offline bool) error {
	builder, virtioJournalConfig, err := newQemuBuilder(true, outdir)
	if err != nil {
		return err
	}
	inst.Builder = builder
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

	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)
	sshPubKey := ignv3types.SSHAuthorizedKey(strings.TrimSpace(string(sshPubKeyBuf)))
	liveConfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Passwd: ignv3types.Passwd{
			Users: []ignv3types.PasswdUser{
				{
					Name: "core",
					SSHAuthorizedKeys: []ignv3types.SSHAuthorizedKey{
						sshPubKey,
					},
				},
			},
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
	liveConfig = ignv3.Merge(*virtioJournalConfig, liveConfig)

	targetConfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Passwd: ignv3types.Passwd{
			Users: []ignv3types.PasswdUser{
				{
					Name: "core",
					SSHAuthorizedKeys: []ignv3types.SSHAuthorizedKey{
						sshPubKey,
					},
				},
			},
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
	targetConfig = ignv3.Merge(*virtioJournalConfig, targetConfig)

	mach, err := inst.InstallViaISOEmbed(nil, liveConfig, targetConfig, offline)
	if err != nil {
		return errors.Wrapf(err, "running iso install")
	}
	defer mach.Destroy()

	return awaitCompletion(mach.QemuInst, outdir, completionChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveLogin(outdir string) error {
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return err
	}
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder := newBaseQemuBuilder()
	// See AddInstallISO, but drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	builder.AddInstallIso(isopath, "")

	completionChannel, err := builder.VirtioChannelRead("coreos.liveiso-success")
	if err != nil {
		return err
	}

	// No network device to test https://github.com/coreos/fedora-coreos-config/pull/326
	builder.Append("-net", "none")

	mach, err := builder.Exec()
	if err != nil {
		return errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(mach, outdir, completionChannel, []string{"coreos-liveiso-success"})
}
