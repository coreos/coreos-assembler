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
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/harness"
	"github.com/coreos/coreos-assembler/mantle/harness/reporters"
	"github.com/coreos/coreos-assembler/mantle/harness/testresult"
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
	isISOFromRAM     bool

	// These tests only run on RHCOS
	tests_RHCOS_uefi = []string{
		"iso-fips.uefi",
	}

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
		"iso-offline-install-fromram.4k.uefi",
		"iso-offline-install-iscsi.bios",
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
		"iso-live-login.s390fw",
		"iso-offline-install.s390fw",
		// https://github.com/coreos/fedora-coreos-tracker/issues/1434
		// "iso-offline-install.mpath.s390fw",
		// https://github.com/coreos/fedora-coreos-tracker/issues/1261
		// "iso-offline-install.4k.s390fw",
		"pxe-online-install.s390fw",
		"pxe-offline-install.s390fw",
		"miniso-install.s390fw",
		"miniso-install.nm.s390fw",
		"miniso-install.4k.nm.s390fw",
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.bios",
	}
	tests_ppc64le = []string{
		"iso-live-login.ppcfw",
		"iso-offline-install.ppcfw",
		"iso-offline-install.mpath.ppcfw",
		"iso-offline-install-fromram.4k.ppcfw",
		"miniso-install.ppcfw",
		"miniso-install.nm.ppcfw",
		"miniso-install.4k.ppcfw",
		"miniso-install.4k.nm.ppcfw",
		"pxe-online-install.ppcfw",
		"pxe-offline-install.4k.ppcfw",
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.bios",
	}
	tests_aarch64 = []string{
		"iso-live-login.uefi",
		"iso-live-login.4k.uefi",
		"iso-offline-install.uefi",
		"iso-offline-install.mpath.uefi",
		"iso-offline-install-fromram.4k.uefi",
		"miniso-install.uefi",
		"miniso-install.nm.uefi",
		"miniso-install.4k.uefi",
		"miniso-install.4k.nm.uefi",
		"pxe-offline-install.uefi",
		"pxe-offline-install.4k.uefi",
		"pxe-online-install.uefi",
		"pxe-online-install.4k.uefi",
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.bios",
	}
)

const (
	installTimeoutMins = 10
	// https://github.com/coreos/fedora-coreos-config/pull/2544
	liveISOFromRAMKarg = "coreos.liveiso.fromram"
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
ExecStart=/bin/sh -c "/usr/bin/jq -er '.[\"build\"]? + .[\"version\"]? == \"%s\"' /mnt/.coreos-aleph-version.json"
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

// This test is broken. Please fix!
// https://github.com/coreos/coreos-assembler/issues/3554
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

// Unit to check that /run/media/iso is not mounted when
// coreos.liveiso.fromram kernel argument is passed
var isoNotMountedUnit = `[Unit]
Description=Verify ISO is not mounted when coreos.liveiso.fromram
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionKernelCommandLine=coreos.liveiso.fromram
[Service]
Type=oneshot
StandardOutput=kmsg+console
StandardError=kmsg+console
RemainAfterExit=yes
# Would like to use SuccessExitStatus but it doesn't support what
# we want: https://github.com/systemd/systemd/issues/10297#issuecomment-1672002635
ExecStart=bash -c "if mountpoint /run/media/iso 2>/dev/null; then exit 1; fi"
[Install]
RequiredBy=coreos-installer.target`

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

var nmstateConfigFile = "/etc/nmstate/br-ex.yml"
var nmstateConfig = `interfaces:
 - name: br-ex
   type: linux-bridge
   state: up
   ipv4:
     enabled: false
   ipv6:
     enabled: false
   bridge:
     port: []
`

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
# Also verify nmstate config
ExecStart=/usr/bin/nmcli c show br-ex
[Install]
# for live system
RequiredBy=coreos-installer.target
# for target system
RequiredBy=multi-user.target`, nmConnectionId, nmConnectionFile)

//go:embed resources/iscsi_butane_setup.yaml
var iscsi_butane_config string

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

func getAllTests(build *util.LocalBuild) []string {
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
	if kola.CosaBuild.Meta.Name == "rhcos" && arch != "s390x" && arch != "ppc64le" {
		tests = append(tests, tests_RHCOS_uefi...)
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

	err = forwardJournal(outdir, builder, config)
	if err != nil {
		return nil, nil, err
	}

	return builder, config, nil
}

func forwardJournal(outdir string, builder *platform.QemuBuilder, config *conf.Conf) error {
	journalPipe, err := builder.VirtioJournal(config, "")
	if err != nil {
		return err
	}
	journalOut, err := os.OpenFile(filepath.Join(outdir, "journal.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	go func() {
		_, err := io.Copy(journalOut, journalPipe)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	return nil
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

func runTestIso(cmd *cobra.Command, args []string) (err error) {
	if kola.CosaBuild == nil {
		return fmt.Errorf("Must provide --build")
	}
	tests := getAllTests(kola.CosaBuild)
	if len(args) != 0 {
		if tests, err = filterTests(tests, args); err != nil {
			return err
		} else if len(tests) == 0 {
			return harness.SuiteEmpty
		}
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

	// see similar code in suite.go
	reportDir := filepath.Join(outputDir, "reports")
	if err := os.Mkdir(reportDir, 0777); err != nil {
		return err
	}

	reporter := reporters.NewJSONReporter("report.json", "testiso", "")
	defer func() {
		if reportErr := reporter.Output(reportDir); reportErr != nil && err != nil {
			err = reportErr
		}
	}()

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
		// For offline it is a part of the first component. i.e. for
		// iso-offline-install.bios we need to search for 'offline' in
		// iso-offline-install, which is currently in components[0].
		if kola.HasString("offline", strings.Split(components[0], "-")) {
			isOffline = true
		}
		// For fromram it is a part of the first component. i.e. for
		// iso-offline-install-fromram.uefi we need to search for 'fromram' in
		// iso-offline-install-fromram, which is currently in components[0].
		if kola.HasString("fromram", strings.Split(components[0], "-")) {
			isISOFromRAM = true
		}

		switch components[0] {
		case "pxe-offline-install", "pxe-online-install":
			duration, err = testPXE(ctx, inst, filepath.Join(outputDir, test))
		case "iso-as-disk":
			duration, err = testAsDisk(ctx, filepath.Join(outputDir, test))
		case "iso-live-login":
			duration, err = testLiveLogin(ctx, filepath.Join(outputDir, test))
		case "iso-fips":
			duration, err = testLiveFIPS(ctx, filepath.Join(outputDir, test))
		case "iso-install", "iso-offline-install", "iso-offline-install-fromram":
			duration, err = testLiveIso(ctx, inst, filepath.Join(outputDir, test), false)
		case "miniso-install":
			duration, err = testLiveIso(ctx, inst, filepath.Join(outputDir, test), true)
		case "iso-offline-install-iscsi":
			duration, err = testLiveInstalliscsi(ctx, inst, filepath.Join(outputDir, test))
		default:
			plog.Fatalf("Unknown test name:%s", test)
		}

		result := testresult.Pass
		output := []byte{}
		if err != nil {
			result = testresult.Fail
			output = []byte(err.Error())
		}
		reporter.ReportTest(test, []string{}, result, duration, output)
		if printResult(test, duration, err) {
			atLeastOneFailed = true
		}
	}

	reporter.SetResult(testresult.Pass)
	if atLeastOneFailed {
		reporter.SetResult(testresult.Fail)
		return harness.SuiteFailed
	}

	return nil
}

func awaitCompletion(ctx context.Context, inst *platform.QemuInstance, outdir string, qchan *os.File, booterrchan chan error, expected []string) (time.Duration, error) {
	start := time.Now()
	errchan := make(chan error)
	go func() {
		timeout := (time.Duration(installTimeoutMins*(100+kola.Options.ExtendTimeoutPercent)) * time.Minute) / 100
		time.Sleep(timeout)
		errchan <- fmt.Errorf("timed out after %v", timeout)
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
		plog.Debugf("qemu exited err=%v", err)
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
			plog.Debugf("Matched expected message %s", exp)
		}
		plog.Debugf("Matched all expected messages")
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

	var isoKernelArgs []string
	var keys []string
	keys = append(keys, strings.TrimSpace(string(sshPubKeyBuf)))
	virtioJournalConfig.AddAuthorizedKeys("core", keys)

	liveConfig := *virtioJournalConfig
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	liveConfig.AddSystemdUnit("iso-not-mounted-when-fromram.service", isoNotMountedUnit, conf.Enable)
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
		// NM keyfile via `iso network embed`
		inst.NmKeyfiles[nmConnectionFile] = nmConnection
		// nmstate config via live Ignition config, propagated via
		// --copy-network, which is enabled by inst.NmKeyfiles
		liveConfig.AddFile(nmstateConfigFile, nmstateConfig, 0644)
	}

	if isISOFromRAM {
		isoKernelArgs = append(isoKernelArgs, liveISOFromRAMKarg)
	}

	// Sometimes the logs that stream from various virtio streams can be
	// incomplete because they depend on services inside the guest.
	// When you are debugging earlyboot/initramfs issues this can be
	// problematic. Let's add a hook here to enable more debugging.
	if _, ok := os.LookupEnv("COSA_TESTISO_DEBUG"); ok {
		isoKernelArgs = append(isoKernelArgs, "systemd.log_color=0 systemd.log_level=debug systemd.log_target=console")
	}

	mach, err := inst.InstallViaISOEmbed(isoKernelArgs, liveConfig, targetConfig, outdir, isOffline, minimal)
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

// testLiveFIPS verifies that adding fips=1 to the ISO results in a FIPS mode system
func testLiveFIPS(ctx context.Context, outdir string) (time.Duration, error) {
	tmpd, err := os.MkdirTemp("", "kola-testiso")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpd)

	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, config, err := newQemuBuilder(outdir)
	if err != nil {
		return 0, err
	}
	defer builder.Close()
	if err := builder.AddIso(isopath, "", false); err != nil {
		return 0, err
	}

	// This is the core change under test - adding the `fips=1` kernel argument via
	// coreos-installer iso kargs modify should enter fips mode.
	// Removing this line should cause this test to fail.
	builder.AppendKernelArgs = "fips=1"

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return 0, err
	}

	config.AddSystemdUnit("fips-verify.service", `
[Unit]
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=fips-signal-ok.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=grep 1 /proc/sys/crypto/fips_enabled
ExecStart=grep FIPS etc/crypto-policies/config

[Install]
RequiredBy=fips-signal-ok.service
`, conf.Enable)
	config.AddSystemdUnit("fips-signal-ok.service", liveSignalOKUnit, conf.Enable)
	config.AddSystemdUnit("fips-emergency-target.service", signalFailureUnit, conf.Enable)

	// Just for reliability, we'll run fully offline
	builder.Append("-net", "none")

	builder.SetConfig(config)
	mach, err := builder.Exec()
	if err != nil {
		return 0, errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{liveOKSignal})
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

// iscsi_butane_setup.yaml contain the full butane config but here is an overview of the setup
// 1 - Boot a live ISO with two extra 10G disks with labels "target" and "var"
//   - Format and mount `virtio-var` to var
//
// 2 - target.container -> start an iscsi target, using quay.io/jbtrystram/targetcli
// 3 - setup-targetcli.service calls /usr/local/bin/targetcli_script:
//   - instructs targetcli to serve /dev/disk/by-id/virtio-target as an iscsi target
//   - disables authentication
//   - verifies the iscsi service is active and reachable
//
// 4 - install-coreos-to-iscsi-target.service calls /usr/local/bin/install-coreos-iscsi:
//   - mount iscsi target
//   - run coreos-installer on the mounted block device
//   - unmount iscsi
//
// 5 - coreos-iscsi-vm.container start a coreos-assemble:
//   - launch cosa qemuexec instructing it to boot from an iPXE script
//     wich in turns mount the iscsi target and load kernel
//   - note the virtserial port device: we pass through the serial port that was created by kola for test completion
//
// 6 - /mnt/workdir-tmp/nested-ign.json contains an ignition config:
//   - when the system is booted, write a success string to /dev/virtio-ports/testisocompletion
//   - As this serial device is mapped to the host serial device, the test concludes
func testLiveInstalliscsi(ctx context.Context, inst platform.Install, outdir string) (time.Duration, error) {

	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, err := newBaseQemuBuilder(outdir)
	if err != nil {
		return 0, err
	}
	defer builder.Close()
	if err := builder.AddIso(isopath, "", false); err != nil {
		return 0, err
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		return 0, err
	}

	// Create a serial channel to read the logs from the nested VM
	nestedVmLogsChannel, err := builder.VirtioChannelRead("nestedvmlogs")
	if err != nil {
		return 0, err
	}

	// Create a file to write the contents of the serial channel into
	nestedVMConsole, err := os.OpenFile(filepath.Join(outdir, "nested_vm_console.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return 0, err
	}

	go func() {
		_, err := io.Copy(nestedVMConsole, nestedVmLogsChannel)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	// empty disk to use as an iscsi target to install coreOS on and subseqently boot
	// Also add a 10G disk that we will mount on /var, to increase space available when pulling containers
	err = builder.AddDisksFromSpecs([]string{"10G:serial=target", "10G:serial=var"})
	if err != nil {
		return 0, err
	}

	// We need more memory to start another VM within !
	builder.MemoryMiB = 2048

	var iscsiTargetConfig = conf.Butane(iscsi_butane_config)

	config, err := iscsiTargetConfig.Render(conf.FailWarnings)
	if err != nil {
		return 0, err
	}
	err = forwardJournal(outdir, builder, config)
	if err != nil {
		return 0, err
	}

	// Add a failure target to stop the test if something go wrong rather than waiting for the 10min timeout
	config.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	// enable network
	builder.EnableUsermodeNetworking([]platform.HostForwardPort{}, "")

	// keep auto-login enabled for easier debug when running console
	config.AddAutoLogin()

	builder.SetConfig(config)

	mach, err := builder.Exec()
	if err != nil {
		return 0, errors.Wrapf(err, "running iso")
	}
	defer mach.Destroy()

	return awaitCompletion(ctx, mach, outdir, completionChannel, nil, []string{"iscsi-boot-ok"})
}
