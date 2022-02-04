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
	"context"
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

	addNmKeyfile bool
)

const (
	installTimeout = 10 * time.Minute

	scenarioPXEInstall = "pxe-install"
	scenarioISOInstall = "iso-install"

	scenarioMinISOInstall   = "miniso-install"
	scenarioMinISOInstallNm = "miniso-install-nm"

	scenarioPXEOfflineInstall = "pxe-offline-install"
	scenarioISOOfflineInstall = "iso-offline-install"

	scenarioISOLiveLogin = "iso-live-login"
	scenarioISOAsDisk    = "iso-as-disk"
)

var allScenarios = map[string]bool{
	scenarioPXEInstall:        true,
	scenarioPXEOfflineInstall: true,
	scenarioISOInstall:        true,
	scenarioISOOfflineInstall: true,
	scenarioMinISOInstall:     true,
	scenarioMinISOInstallNm:   true,
	scenarioISOLiveLogin:      true,
	scenarioISOAsDisk:         true,
}

var liveOKSignal = "live-test-OK"
var liveSignalOKUnit = fmt.Sprintf(`[Unit]
Description=TestISO Signal Live ISO Completion
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
[Install]
# for install scenarios
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target
`, liveOKSignal)

var downloadCheck = `[Unit]
Description=TestISO Verify CoreOS Installer Download
After=coreos-installer.service
Before=coreos-installer.target
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
`

var signalCompleteString = "coreos-installer-test-OK"
var signalCompletionUnit = fmt.Sprintf(`[Unit]
Description=TestISO Signal Completion
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

var checkNoIgnition = fmt.Sprintf(`[Unit]
Description=TestISO Verify No Ignition Config
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
After=coreos-ignition-firstboot-complete.service
RequiresMountsFor=/boot
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '[ ! -e /boot/ignition ]'
[Install]
RequiredBy=multi-user.target`)

var multipathedRoot = fmt.Sprintf(`[Unit]
Description=TestISO Verify Multipathed Root
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c '[[ $(findmnt -nvro SOURCE /sysroot) == /dev/mapper/mpatha4 ]]'
[Install]
RequiredBy=multi-user.target`)

var verifyNoEFIBootEntry = fmt.Sprintf(`[Unit]
Description=TestISO Verify No EFI Boot Entry
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionPathExists=/sys/firmware/efi
Before=live-signal-ok.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '! efibootmgr -v | grep -E "(HD|CDROM)\("'
[Install]
# for install scenarios
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`)

var nmConnectionId = "CoreOS DHCP"
var nmConnectionFile = "coreos-dhcp.nmconnection"
var nmConnection = fmt.Sprintf(`[connection]
id=%s
type=ethernet

[ipv4]
method=auto
`, nmConnectionId)

// This is used to verify *both* the live and the target system in the `--add-nm-keyfile` path.
var verifyNmKeyfile = fmt.Sprintf(`[Unit]
Description=TestISO Verify NM Keyfile Propagation
OnFailure=emergency.target
OnFailureJobMode=isolate
Wants=network-online.target
After=network-online.target
Before=live-signal-ok.service
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/journalctl -u nm-initrd --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/journalctl -u NetworkManager --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/grep "%[1]s" /etc/NetworkManager/system-connections/%[2]s
[Install]
# for live system
RequiredBy=coreos-installer.target
# for target system
RequiredBy=multi-user.target`, nmConnectionId, nmConnectionFile)

func init() {
	cmdTestIso.Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	cmdTestIso.Flags().BoolVarP(&nopxe, "no-pxe", "P", false, "Skip testing live installer PXE")
	cmdTestIso.Flags().BoolVarP(&noiso, "no-iso", "", false, "Skip testing live installer ISO")
	cmdTestIso.Flags().BoolVar(&console, "console", false, "Connect qemu console to terminal, turn off automatic initramfs failure checking")
	cmdTestIso.Flags().BoolVar(&pxeAppendRootfs, "pxe-append-rootfs", false, "Append rootfs to PXE initrd instead of fetching at runtime")
	cmdTestIso.Flags().StringSliceVar(&pxeKernelArgs, "pxe-kargs", nil, "Additional kernel arguments for PXE")
	cmdTestIso.Flags().BoolVar(&addNmKeyfile, "add-nm-keyfile", false, "Add NetworkManager connection keyfile")
	cmdTestIso.Flags().StringSliceVar(&scenarios, "scenarios", []string{scenarioPXEInstall, scenarioISOOfflineInstall, scenarioPXEOfflineInstall, scenarioISOLiveLogin, scenarioISOAsDisk, scenarioMinISOInstall, scenarioMinISOInstallNm}, fmt.Sprintf("Test scenarios (also available: %v)", []string{scenarioISOInstall}))
	cmdTestIso.Args = cobra.ExactArgs(0)

	root.AddCommand(cmdTestIso)
}

func newBaseQemuBuilder(outdir string) (*platform.QemuBuilder, error) {
	builder := platform.NewMetalQemuBuilderDefault()
	builder.Firmware = kola.QEMUOptions.Firmware

	if err := os.MkdirAll(outdir, 0755); err != nil {
		return nil, err
	}

	builder.InheritConsole = console
	if !console {
		builder.ConsoleFile = filepath.Join(outdir, "console.txt")
	}

	return builder, nil
}

func newQemuBuilder(outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, err := newBaseQemuBuilder(outdir)
	if err != nil {
		return nil, nil, err
	}

	config, err := conf.EmptyIgnition().Render(conf.FailWarnings)
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

func newQemuBuilderWithDisk(outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, config, err := newQemuBuilder(outdir)
	if err != nil {
		return nil, nil, err
	}

	sectorSize := 0
	if kola.QEMUOptions.Native4k {
		sectorSize = 4096
	}

	disk := platform.Disk{
		Size:       "12G", // Arbitrary
		SectorSize: sectorSize,

		MultiPathDisk: kola.QEMUOptions.MultiPathDisk,
	}

	//TBD: see if we can remove this and just use AddDisk and inject bootindex during startup
	if system.RpmArch() == "s390x" || system.RpmArch() == "aarch64" {
		// s390x and aarch64 need to use bootindex as they don't support boot once
		if err := builder.AddDisk(&disk); err != nil {
			return nil, nil, err
		}
	} else {
		if err := builder.AddPrimaryDisk(&disk); err != nil {
			return nil, nil, err
		}
	}

	return builder, config, nil
}

func runTestIso(cmd *cobra.Command, args []string) error {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --build")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		delete(targetScenarios, scenarioMinISOInstall)
		delete(targetScenarios, scenarioMinISOInstallNm)
		delete(targetScenarios, scenarioISOLiveLogin)
	}

	// just make it a normal print message so pipelines don't error out for ppc64le and s390x
	if len(targetScenarios) == 0 {
		fmt.Println("No valid scenarios specified!")
		return nil
	}
	scenarios = []string{}
	for scenario := range targetScenarios {
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
		MultiPathDisk:   kola.QEMUOptions.MultiPathDisk,
		PxeAppendRootfs: pxeAppendRootfs,
		NmKeyfiles:      make(map[string]string),
	}

	if instInsecure {
		baseInst.Insecure = true
		fmt.Printf("Ignoring verification of signature on metal image\n")
	}

	// Ignore signing verification by default when running with development build
	// https://github.com/coreos/fedora-coreos-tracker/issues/908
	if !baseInst.Insecure && strings.Contains(kola.CosaBuild.Meta.BuildID, ".dev.") {
		baseInst.Insecure = true
		fmt.Printf("Detected development build; disabling signature verification\n")
	}

	ranTest := false

	if _, ok := targetScenarios[scenarioPXEInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
			return fmt.Errorf("build %s has no live installer kernel", kola.CosaBuild.Meta.Name)
		}

		ranTest = true
		instPxe := baseInst // Pretend this is Rust and I wrote .copy()

		if err := testPXE(ctx, instPxe, filepath.Join(outputDir, scenarioPXEInstall), false); err != nil {
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

		if err := testPXE(ctx, instPxe, filepath.Join(outputDir, scenarioPXEOfflineInstall), true); err != nil {
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
		if err := testLiveIso(ctx, instIso, filepath.Join(outputDir, scenarioISOInstall), false, false); err != nil {
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
		if err := testLiveIso(ctx, instIso, filepath.Join(outputDir, scenarioISOOfflineInstall), true, false); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioISOOfflineInstall)
		}
		printSuccess(scenarioISOOfflineInstall)
	}
	if _, ok := targetScenarios[scenarioISOLiveLogin]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		if err := testLiveLogin(ctx, filepath.Join(outputDir, scenarioISOLiveLogin)); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioISOLiveLogin)
		}
		printSuccess(scenarioISOLiveLogin)
	}
	if _, ok := targetScenarios[scenarioISOAsDisk]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		switch system.RpmArch() {
		case "x86_64":
			ranTest = true
			if err := testAsDisk(ctx, filepath.Join(outputDir, scenarioISOAsDisk)); err != nil {
				return errors.Wrapf(err, "scenario %s", scenarioISOAsDisk)
			}
			printSuccess(scenarioISOAsDisk)
		default:
			// no hybrid partition table to boot from
			fmt.Printf("%s unsupported on %s; skipping\n", scenarioISOAsDisk, system.RpmArch())
		}
	}
	if _, ok := targetScenarios[scenarioMinISOInstall]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		instIso := baseInst // Pretend this is Rust and I wrote .copy()
		if err := testLiveIso(ctx, instIso, filepath.Join(outputDir, scenarioMinISOInstall), false, true); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioMinISOInstall)
		}
		printSuccess(scenarioMinISOInstall)
	}
	if _, ok := targetScenarios[scenarioMinISOInstallNm]; ok {
		if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
			return fmt.Errorf("build %s has no live ISO", kola.CosaBuild.Meta.Name)
		}
		ranTest = true
		instIso := baseInst // Pretend this is Rust and I wrote .copy()
		addNmKeyfile = true
		if err := testLiveIso(ctx, instIso, filepath.Join(outputDir, scenarioMinISOInstallNm), false, true); err != nil {
			return errors.Wrapf(err, "scenario %s", scenarioMinISOInstallNm)
		}
		printSuccess(scenarioMinISOInstallNm)
	}

	if !ranTest {
		panic("Nothing was tested!")
	}

	return nil
}

func awaitCompletion(ctx context.Context, inst *platform.QemuInstance, outdir string, qchan *os.File, booterrchan chan error, expected []string) error {
	errchan := make(chan error)
	go func() {
		time.Sleep(installTimeout)
		errchan <- fmt.Errorf("timed out after %v", installTimeout)
	}()
	if !console {
		go func() {
			errBuf, err := inst.WaitIgnitionError(ctx)
			if err == nil {
				if errBuf != "" {
					plog.Info("entered emergency.target in initramfs")
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
		err := inst.Wait()
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
	go func() {
		//check for error when switching boot order
		if booterrchan != nil {
			if err := <-booterrchan; err != nil {
				errchan <- err
			}
		}
	}()
	return <-errchan
}

func printSuccess(mode string) {
	metaltype := "metal"
	if kola.QEMUOptions.Native4k {
		metaltype = "metal4k"
	}
	onMultipath := ""
	if kola.QEMUOptions.MultiPathDisk {
		onMultipath = " on multipath"
	}
	withNmKeyfile := ""
	if addNmKeyfile {
		withNmKeyfile = " with NM keyfile"
	}
	fmt.Printf("Successfully tested scenario %s for %s on %s (%s%s%s)\n", mode, kola.CosaBuild.Meta.OstreeVersion, kola.QEMUOptions.Firmware, metaltype, onMultipath, withNmKeyfile)
}

func testPXE(ctx context.Context, inst platform.Install, outdir string, offline bool) error {
	if addNmKeyfile {
		return errors.New("--add-nm-keyfile not yet supported for PXE")
	}
	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}

	builder, virtioJournalConfig, err := newQemuBuilderWithDisk(outdir)
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
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)

	mach, err := inst.PXE(pxeKernelArgs, liveConfig, targetConfig, offline)
	if err != nil {
		return errors.Wrapf(err, "running PXE")
	}
	defer func() {
		if err := mach.Destroy(); err != nil {
			plog.Errorf("Failed to destroy PXE: %v", err)
		}
	}()

	return awaitCompletion(ctx, mach.QemuInst, outdir, completionChannel, mach.BootStartedErrorChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveIso(ctx context.Context, inst platform.Install, outdir string, offline, minimal bool) error {
	tmpd, err := ioutil.TempDir("", "kola-testiso")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return err
	}

	builder, virtioJournalConfig, err := newQemuBuilderWithDisk(outdir)
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
	liveConfig.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)
	if inst.MultiPathDisk {
		targetConfig.AddSystemdUnit("coreos-test-installer-multipathed.service", multipathedRoot, conf.Enable)
	}

	if addNmKeyfile {
		liveConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		targetConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		inst.NmKeyfiles[nmConnectionFile] = nmConnection
	}

	mach, err := inst.InstallViaISOEmbed(nil, liveConfig, targetConfig, outdir, offline, minimal)
	if err != nil {
		return errors.Wrapf(err, "running iso install")
	}
	defer func() {
		if err := mach.Destroy(); err != nil {
			plog.Errorf("Failed to destroy iso: %v", err)
		}
	}()

	return awaitCompletion(ctx, mach.QemuInst, outdir, completionChannel, mach.BootStartedErrorChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveLogin(ctx context.Context, outdir string) error {
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, err := newBaseQemuBuilder(outdir)
	if err != nil {
		return nil
	}
	defer builder.Close()
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	if err := builder.AddIso(isopath, "", false); err != nil {
		return err
	}

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

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{"coreos-liveiso-success"})
}

func testAsDisk(ctx context.Context, outdir string) error {
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, config, err := newQemuBuilder(outdir)
	if err != nil {
		return nil
	}
	defer builder.Close()
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	if err := builder.AddIso(isopath, "", true); err != nil {
		return err
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return err
	}

	config.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	builder.SetConfig(config)

	mach, err := builder.Exec()
	if err != nil {
		return errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{liveOKSignal})
}
