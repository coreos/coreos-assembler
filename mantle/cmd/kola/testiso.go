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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

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

	nopxe bool
	noiso bool

	scenarios []string

	pxeAppendRootfs bool
	pxeKernelArgs   []string

	console bool
)

const (
	installTimeout = 10 * time.Minute

	scenarioPXEInstall = "pxe-install"
	scenarioISOInstall = "iso-install"

	scenarioPXEOfflineInstall = "pxe-offline-install"
	scenarioISOOfflineInstall = "iso-offline-install"
	scenarioISOLiveLogin      = "iso-live-login"
)

var allScenarios = map[string]bool{
	scenarioPXEInstall:        true,
	scenarioPXEOfflineInstall: true,
	scenarioISOInstall:        true,
	scenarioISOOfflineInstall: true,
	scenarioISOLiveLogin:      true,
}

var liveOKSignal = "live-test-OK"
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
# In the embedded ISO scenario we're using the default multi-user.target
# because we write out and enable our own coreos-installer service units
RequiredBy=multi-user.target
# In the PXE case we are passing kargs and the coreos-installer-generator
# will switch us to target coreos-installer.target
RequiredBy=coreos-installer.target
`, liveOKSignal)

var downloadCheck = `[Unit]
After=coreos-installer.service
Before=coreos-installer.target
# Can be dropped with coreos-installer v0.5.1
Before=coreos-installer-reboot.service
[Service]
Type=oneshot
StandardOutput=kmsg+console
StandardError=kmsg+console
ExecStart=/bin/sh -c "journalctl -t coreos-installer-service | /usr/bin/awk '/[Dd]ownload/ {exit 1}'"
ExecStart=/bin/sh -c "/usr/bin/udevadm settle"
ExecStart=/bin/sh -c "/usr/bin/mount /dev/disk/by-label/root /mnt"
ExecStart=/bin/sh -c "/usr/bin/jq -er '.[\"build\"] == \"%s\"' /mnt/.coreos-aleph-version.json"
ExecStart=/bin/sh -c "/usr/bin/jq -er '.[\"ostree-commit\"] == \"%s\"' /mnt/.coreos-aleph-version.json"
[Install]
RequiredBy=coreos-installer.target
# Can be dropped when the target is fixed to not trigger reboot when a new unit is added to the target and fails
RequiredBy=coreos-installer-reboot.service
`

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
	cmdTestIso.Flags().BoolVarP(&nopxe, "no-pxe", "P", false, "Skip testing live installer PXE")
	cmdTestIso.Flags().BoolVarP(&noiso, "no-iso", "", false, "Skip testing live installer ISO")
	cmdTestIso.Flags().BoolVar(&console, "console", false, "Display qemu console to stdout, turn off automatic initramfs failure checking")
	cmdTestIso.Flags().BoolVar(&pxeAppendRootfs, "pxe-append-rootfs", false, "Append rootfs to PXE initrd instead of fetching at runtime")
	cmdTestIso.Flags().StringSliceVar(&pxeKernelArgs, "pxe-kargs", nil, "Additional kernel arguments for PXE")
	// FIXME move scenarioISOLiveLogin into the defaults once https://github.com/coreos/fedora-coreos-config/pull/339#issuecomment-613000050 is fixed
	cmdTestIso.Flags().StringSliceVar(&scenarios, "scenarios", []string{scenarioPXEInstall, scenarioISOOfflineInstall, scenarioPXEOfflineInstall}, fmt.Sprintf("Test scenarios (also available: %v)", []string{scenarioISOLiveLogin, scenarioISOInstall}))
	cmdTestIso.Args = cobra.ExactArgs(0)

	root.AddCommand(cmdTestIso)
}

func newBaseQemuBuilder() *platform.QemuBuilder {
	builder := platform.NewMetalQemuBuilderDefault()
	builder.Firmware = kola.QEMUOptions.Firmware

	builder.InheritConsole = console

	return builder
}

func newQemuBuilder(outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder := newBaseQemuBuilder()
	sectorSize := 0
	if kola.QEMUOptions.Native4k {
		sectorSize = 4096
	}

	//TBD: see if we can remove this and just use AddDisk and inject bootindex during startup
	if system.RpmArch() == "s390x" || system.RpmArch() == "aarch64" {
		// s390x and aarch64 need to use bootindex as they don't support boot once
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
	config, err := conf.EmptyIgnition().Render(kola.IsIgnitionV2())
	if err != nil {
		return nil, nil, err
	}
	journalPipe, err := builder.VirtioJournal(config, "")
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

	return builder, config, nil
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

	// s390x: iso-install does not work because s390x uses an El Torito image
	if system.RpmArch() == "s390x" {
		fmt.Println("Skipping iso-install on s390x")
		noiso = true
	}

	if nopxe {
		delete(targetScenarios, scenarioPXEInstall)
		delete(targetScenarios, scenarioPXEOfflineInstall)
	}
	if noiso {
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
		CosaBuild:       kola.CosaBuild,
		Native4k:        kola.QEMUOptions.Native4k,
		PxeAppendRootfs: pxeAppendRootfs,

		IgnitionSpec2: kola.IsIgnitionV2(),
	}

	if instInsecure {
		baseInst.Insecure = true
	}

	ranTest := false

	if _, ok := targetScenarios[scenarioPXEInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Meta.Name)
		}

		ranTest = true
		instPxe := baseInst // Pretend this is Rust and I wrote .copy()

		if err := testPXE(instPxe, filepath.Join(outputDir, scenarioPXEInstall), false); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioPXEInstall)

		}
		printSuccess(scenarioPXEInstall)
	}
	if _, ok := targetScenarios[scenarioPXEOfflineInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Meta.Name)
		}

		ranTest = true
		instPxe := baseInst // Pretend this is Rust and I wrote .copy()

		if err := testPXE(instPxe, filepath.Join(outputDir, scenarioPXEOfflineInstall), true); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioPXEOfflineInstall)

		}
		printSuccess(scenarioPXEOfflineInstall)
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
	if !console {
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
			// switch the boot order here, we are well into the installation process - only for aarch64 and s390x
			if line == liveOKSignal {
				if err := inst.SwitchBootOrder(); err != nil {
					errchan <- errors.Wrapf(err, "switching boot order failed")
					return
				}
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

func testPXE(inst platform.Install, outdir string, offline bool) error {
	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}

	builder, virtioJournalConfig, err := newQemuBuilder(outdir)
	if err != nil {
		return err
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)

	if offline {
		contents := fmt.Sprintf(downloadCheck, kola.CosaBuild.Meta.BuildID, kola.CosaBuild.Meta.OstreeCommit)
		liveConfig.AddSystemdUnit("coreos-installer-offline-check.service", contents, conf.Enable)
	}

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)

	mach, err := inst.PXE(pxeKernelArgs, liveConfig, targetConfig, offline)
	if err != nil {
		return errors.Wrapf(err, "running PXE")
	}
	defer mach.Destroy()

	return awaitCompletion(mach.QemuInst, outdir, completionChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveIso(inst platform.Install, outdir string, offline bool) error {
	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}

	builder, virtioJournalConfig, err := newQemuBuilder(outdir)
	if err != nil {
		return err
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)

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
	defer builder.Close()
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	builder.AddIso(isopath, "")

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
