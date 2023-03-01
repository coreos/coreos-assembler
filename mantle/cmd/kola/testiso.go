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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/harness"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"

	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

var (
	cmdTestIso = &cobra.Command{
		RunE:    runTestIso,
		PreRunE: preRun,
		Use:     "testiso [glob pattern...]",
		Short:   "Test a CoreOS PXE boot or ISO install path",

		SilenceUsage: true,
	}

	instInsecure bool

	pxeAppendRootfs bool
	pxeKernelArgs   []string

	console bool

	addNmKeyfile     bool
	enable4k         bool
	enableMultipath  bool
	enableUefi       bool
	enableUefiSecure bool
	isOffline        bool

	// The iso-as-disk tests are only supported in x86_64 because other
	// architectures don't have the required hybrid partition table.
	tests_x86_64 = []string{
		"iso-as-disk.bios",
		"iso-as-disk.uefi",
		"iso-as-disk.uefi-secure",
		"iso-as-disk.4k.uefi",
		"iso-install.bios",
		"iso-live-login.bios",
		"iso-live-login.uefi",
		"iso-live-login.uefi-secure",
		"iso-live-login.4k.uefi",
		"iso-offline-install.bios",
		"iso-offline-install.mpath.bios",
		"iso-offline-install.4k.uefi",
		"miniso-install.bios",
		"miniso-install.nm.bios",
		"miniso-install.4k.uefi",
		"miniso-install.4k.nm.uefi",
		"pxe-offline-install.bios",
		"pxe-offline-install.4k.uefi",
		"pxe-online-install.bios",
		"pxe-online-install.4k.uefi",
	}
	tests_s390x = []string{
		"pxe-online-install.s390fw",
		"pxe-offline-install.s390fw",
	}
	tests_ppc64le = []string{
		"iso-live-login.ppcfw",
		"iso-offline-install.ppcfw",
		"iso-offline-install.mpath.ppcfw",
		"iso-offline-install.4k.ppcfw",
		"miniso-install.ppcfw",
		"miniso-install.nm.ppcfw",
		"miniso-install.4k.ppcfw",
		"miniso-install.4k.nm.ppcfw",
		"pxe-online-install.ppcfw",
		"pxe-offline-install.4k.ppcfw",
	}
	tests_aarch64 = []string{
		"iso-live-login.uefi",
		"iso-live-login.4k.uefi",
		"iso-offline-install.uefi",
		"iso-offline-install.mpath.uefi",
		"iso-offline-install.4k.uefi",
		"miniso-install.uefi",
		"miniso-install.nm.uefi",
		"miniso-install.4k.uefi",
		"miniso-install.4k.nm.uefi",
		"pxe-offline-install.uefi",
		"pxe-offline-install.4k.uefi",
		"pxe-online-install.uefi",
		"pxe-online-install.4k.uefi",
	}
)

const (
	installTimeout = 10 * time.Minute
)

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
# for install tests
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

var signalEmergencyString = "coreos-installer-test-entered-emergency-target"
var signalFailureUnit = fmt.Sprintf(`[Unit]
Description=TestISO Signal Failure
Requires=dev-virtio\\x2dports-testisocompletion.device
DefaultDependencies=false
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=emergency.target
`, signalEmergencyString)

var checkNoIgnition = `[Unit]
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
RequiredBy=multi-user.target`

var multipathedRoot = `[Unit]
Description=TestISO Verify Multipathed Root
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c '[[ $(findmnt -nvro SOURCE /sysroot) == /dev/mapper/mpatha4 ]]'
[Install]
RequiredBy=multi-user.target`

var verifyNoEFIBootEntry = `[Unit]
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
# for install tests
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`

var nmConnectionId = "CoreOS DHCP"
var nmConnectionFile = "coreos-dhcp.nmconnection"
var nmConnection = fmt.Sprintf(`[connection]
id=%s
type=ethernet
# add wait-device-timeout here so we make sure NetworkManager-wait-online.service will
# wait for a device to be present before exiting. See
# https://github.com/coreos/fedora-coreos-tracker/issues/1275#issuecomment-1231605438
wait-device-timeout=20000

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
	cmdTestIso.Flags().BoolVar(&console, "console", false, "Connect qemu console to terminal, turn off automatic initramfs failure checking")
	cmdTestIso.Flags().BoolVar(&pxeAppendRootfs, "pxe-append-rootfs", false, "Append rootfs to PXE initrd instead of fetching at runtime")
	cmdTestIso.Flags().StringSliceVar(&pxeKernelArgs, "pxe-kargs", nil, "Additional kernel arguments for PXE")

	root.AddCommand(cmdTestIso)
}

func liveArtifactExistsInBuild() error {

	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		return fmt.Errorf("build %s is missing live artifacts", kola.CosaBuild.Meta.Name)
	}
	return nil
}

func getArchPatternsList() []string {
	arch := coreosarch.CurrentRpmArch()
	var tests []string
	switch arch {
	case "x86_64":
		tests = tests_x86_64
	case "ppc64le":
		tests = tests_ppc64le
	case "s390x":
		tests = tests_s390x
	case "aarch64":
		tests = tests_aarch64
	}
	return tests
}

func newBaseQemuBuilder(outdir string) (*platform.QemuBuilder, error) {
	builder := platform.NewMetalQemuBuilderDefault()
	if enableUefiSecure {
		builder.Firmware = "uefi-secure"
	} else if enableUefi {
		builder.Firmware = "uefi"
	}

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
	if enable4k {
		sectorSize = 4096
	}

	disk := platform.Disk{
		Size:          "12G", // Arbitrary
		SectorSize:    sectorSize,
		MultiPathDisk: enableMultipath,
	}

	//TBD: see if we can remove this and just use AddDisk and inject bootindex during startup
	if coreosarch.CurrentRpmArch() == "s390x" || coreosarch.CurrentRpmArch() == "aarch64" {
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

// See similar semantics in the `filterTests` of `kola.go`.
func filterTests(tests []string, patterns []string) ([]string, error) {
	r := []string{}
	for _, test := range tests {
		if matches, err := kola.MatchesPatterns(test, patterns); err != nil {
			return nil, err
		} else if matches {
			r = append(r, test)
		}
	}
	return r, nil
}

func runTestIso(cmd *cobra.Command, args []string) error {
	var err error
	tests := getArchPatternsList()
	if len(args) != 0 {
		if tests, err = filterTests(tests, args); err != nil {
			return err
		} else if len(tests) == 0 {
			return harness.SuiteEmpty
		}
	}

	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --build")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Call `ParseDenyListYaml` to populate the `kola.DenylistedTests` var
	err = kola.ParseDenyListYaml("qemu")
	if err != nil {
		plog.Fatal(err)
	}

	finalTests := []string{}
	for _, test := range tests {
		if !kola.HasString(test, kola.DenylistedTests) {
			matchTest, err := kola.MatchesPatterns(test, kola.DenylistedTests)
			if err != nil {
				return err

			}
			if !matchTest {
				finalTests = append(finalTests, test)
			}
		}
	}

	// note this reassigns a *global*
	outputDir, err = kola.SetupOutputDir(outputDir, "testiso")
	if err != nil {
		return err
	}

	baseInst := platform.Install{
		CosaBuild:       kola.CosaBuild,
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

	var duration time.Duration

	atLeastOneFailed := false
	for _, test := range finalTests {

		// All of these tests require buildextend-live to have been run
		err = liveArtifactExistsInBuild()
		if err != nil {
			return err
		}

		addNmKeyfile = false
		enable4k = false
		enableMultipath = false
		enableUefi = false
		enableUefiSecure = false
		isOffline = false
		inst := baseInst // Pretend this is Rust and I wrote .copy()

		fmt.Printf("Running test: %s\n", test)
		components := strings.Split(test, ".")

		if kola.HasString("4k", components) {
			enable4k = true
			inst.Native4k = true
		}
		if kola.HasString("nm", components) {
			addNmKeyfile = true
		}
		if kola.HasString("mpath", components) {
			enableMultipath = true
			inst.MultiPathDisk = true
		}
		if kola.HasString("uefi-secure", components) {
			enableUefiSecure = true
		} else if kola.HasString("uefi", components) {
			enableUefi = true
		}
		if kola.HasString("offline", components) {
			isOffline = true
		}

		switch components[0] {
		case "pxe-offline-install", "pxe-online-install":
			duration, err = testPXE(ctx, inst, filepath.Join(outputDir, test))
		case "iso-as-disk":
			duration, err = testAsDisk(ctx, filepath.Join(outputDir, test))
		case "iso-live-login":
			duration, err = testLiveLogin(ctx, filepath.Join(outputDir, test))
		case "iso-install", "iso-offline-install":
			duration, err = testLiveIso(ctx, inst, filepath.Join(outputDir, test), false)
		case "miniso-install":
			duration, err = testLiveIso(ctx, inst, filepath.Join(outputDir, test), true)
		default:
			plog.Fatalf("Unknown test name:%s", test)
		}
		if printResult(test, duration, err) {
			atLeastOneFailed = true
		}
	}

	if atLeastOneFailed {
		return harness.SuiteFailed
	}

	return nil
}

func awaitCompletion(ctx context.Context, inst *platform.QemuInstance, outdir string, qchan *os.File, booterrchan chan error, expected []string) (time.Duration, error) {
	start := time.Now()
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
					if err := os.WriteFile(path, []byte(errBuf), 0644); err != nil {
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
		// only one Wait() gets process data, so also manually check for signal
		if err == nil && inst.Signaled() {
			err = errors.New("process killed")
		}
		if err != nil {
			errchan <- errors.Wrapf(err, "QEMU unexpectedly exited while awaiting completion")
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
					// this may be from QEMU getting killed or exiting; wait a bit
					// to give a chance for .Wait() above to feed the channel with a
					// better error
					time.Sleep(1 * time.Second)
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
	err := <-errchan
	return time.Since(start), err
}

func printResult(test string, duration time.Duration, err error) bool {
	result := "PASS"
	if err != nil {
		result = "FAIL"
	}
	fmt.Printf("%s: %s (%s)\n", result, test, duration.Round(time.Millisecond).String())
	if err != nil {
		fmt.Printf("    %s\n", err)
		return true
	}
	return false
}

func testPXE(ctx context.Context, inst platform.Install, outdir string) (time.Duration, error) {
	if addNmKeyfile {
		return 0, errors.New("--add-nm-keyfile not yet supported for PXE")
	}
	tmpd, err := os.MkdirTemp("", "kola-testiso")
	if err != nil {
		return 0, errors.Wrapf(err, "creating tempdir")
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return 0, errors.Wrapf(err, "creating SSH AuthorizedKey")
	}

	builder, virtioJournalConfig, err := newQemuBuilderWithDisk(outdir)
	if err != nil {
		return 0, errors.Wrapf(err, "creating QemuBuilder")
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return 0, errors.Wrapf(err, "setting up virtio-serial channel")
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	if isOffline {
		contents := fmt.Sprintf(downloadCheck, kola.CosaBuild.Meta.BuildID, kola.CosaBuild.Meta.OstreeCommit)
		liveConfig.AddSystemdUnit("coreos-installer-offline-check.service", contents, conf.Enable)
	}

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)

	mach, err := inst.PXE(pxeKernelArgs, liveConfig, targetConfig, isOffline)
	if err != nil {
		return 0, errors.Wrapf(err, "running PXE")
	}
	defer func() {
		if err := mach.Destroy(); err != nil {
			plog.Errorf("Failed to destroy PXE: %v", err)
		}
	}()

	return awaitCompletion(ctx, mach.QemuInst, outdir, completionChannel, mach.BootStartedErrorChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveIso(ctx context.Context, inst platform.Install, outdir string, minimal bool) (time.Duration, error) {
	tmpd, err := os.MkdirTemp("", "kola-testiso")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpd)

	sshPubKeyBuf, _, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		return 0, err
	}

	builder, virtioJournalConfig, err := newQemuBuilderWithDisk(outdir)
	if err != nil {
		return 0, err
	}
	inst.Builder = builder
	completionChannel, err := inst.Builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return 0, err
	}

	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	targetConfig := *virtioJournalConfig
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)
	if inst.MultiPathDisk {
		targetConfig.AddSystemdUnit("coreos-test-installer-multipathed.service", multipathedRoot, conf.Enable)
	}

	if addNmKeyfile {
		liveConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		targetConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		inst.NmKeyfiles[nmConnectionFile] = nmConnection
	}

	mach, err := inst.InstallViaISOEmbed(nil, liveConfig, targetConfig, outdir, isOffline, minimal)
	if err != nil {
		return 0, errors.Wrapf(err, "running iso install")
	}
	defer func() {
		if err := mach.Destroy(); err != nil {
			plog.Errorf("Failed to destroy iso: %v", err)
		}
	}()

	return awaitCompletion(ctx, mach.QemuInst, outdir, completionChannel, mach.BootStartedErrorChannel, []string{liveOKSignal, signalCompleteString})
}

func testLiveLogin(ctx context.Context, outdir string) (time.Duration, error) {
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, err := newBaseQemuBuilder(outdir)
	if err != nil {
		return 0, err
	}
	defer builder.Close()
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	if err := builder.AddIso(isopath, "", false); err != nil {
		return 0, err
	}

	completionChannel, err := builder.VirtioChannelRead("coreos.liveiso-success")
	if err != nil {
		return 0, err
	}

	// No network device to test https://github.com/coreos/fedora-coreos-config/pull/326
	builder.Append("-net", "none")

	mach, err := builder.Exec()
	if err != nil {
		return 0, errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{"coreos-liveiso-success"})
}

func testAsDisk(ctx context.Context, outdir string) (time.Duration, error) {
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, config, err := newQemuBuilder(outdir)
	if err != nil {
		return 0, err
	}
	defer builder.Close()
	// Drop the bootindex bit (applicable to all arches except s390x and ppc64le); we want it to be the default
	if err := builder.AddIso(isopath, "", true); err != nil {
		return 0, err
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return 0, err
	}

	config.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	builder.SetConfig(config)

	mach, err := builder.Exec()
	if err != nil {
		return 0, errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{liveOKSignal})
}
