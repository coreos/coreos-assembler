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
	"strconv"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/harness"
	"github.com/coreos/coreos-assembler/mantle/harness/reporters"
	"github.com/coreos/coreos-assembler/mantle/harness/testresult"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
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

	pxeKernelArgs []string

	console bool

	enable4k         bool
	enableUefi       bool
	enableUefiSecure bool

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
	}
)

const (
	installTimeoutMins = 12
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

func init() {
	cmdTestIso.Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	cmdTestIso.Flags().BoolVar(&console, "console", false, "Connect qemu console to terminal, turn off automatic initramfs failure checking")
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
	default:
		return []string{}
	}
	if kola.CosaBuild.Meta.Name == "rhcos" && arch != "s390x" && arch != "ppc64le" {
		tests = append(tests, tests_RHCOS_uefi...)
	}
	return tests
}

func newBaseQemuBuilder(outdir string) (*platform.QemuBuilder, error) {
	builder := qemu.NewMetalQemuBuilderDefault()
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

	if kola.QEMUOptions.Memory != "" {
		parsedMem, err := strconv.ParseInt(kola.QEMUOptions.Memory, 10, 32)
		if err != nil {
			return nil, err
		}
		builder.MemoryMiB = int(parsedMem)
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

	baseInst := qemu.Install{
		CosaBuild:  kola.CosaBuild,
		NmKeyfiles: make(map[string]string),
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

		enable4k = false
		enableUefi = false
		enableUefiSecure = false
		inst := baseInst // Pretend this is Rust and I wrote .copy()

		fmt.Printf("Running test: %s\n", test)
		components := strings.Split(test, ".")

		inst.PxeAppendRootfs = kola.HasString("rootfs-appended", components)

		if kola.HasString("4k", components) {
			enable4k = true
			inst.Native4k = true
		}
		if kola.HasString("uefi-secure", components) {
			enableUefiSecure = true
		} else if kola.HasString("uefi", components) {
			enableUefi = true
		}

		switch components[0] {
		case "iso-as-disk":
			duration, err = testAsDisk(ctx, filepath.Join(outputDir, test))
		case "iso-fips":
			duration, err = testLiveFIPS(ctx, filepath.Join(outputDir, test))
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
	elapsed := time.Since(start)
	if err == nil {
		// No error so far, check the console and journal files
		consoleFile := filepath.Join(outdir, "console.txt")
		journalFile := filepath.Join(outdir, "journal.txt")
		files := []string{consoleFile, journalFile}
		for _, file := range files {
			fileName := filepath.Base(file)
			// Check if the file exists
			_, err := os.Stat(file)
			if os.IsNotExist(err) {
				fmt.Printf("The file: %v does not exist\n", fileName)
				continue
			} else if err != nil {
				fmt.Println(err)
				return elapsed, err
			}
			// Read the contents of the file
			fileContent, err := os.ReadFile(file)
			if err != nil {
				fmt.Println(err)
				return elapsed, err
			}
			// Check for badness with CheckConsole
			warnOnly, badlines := kola.CheckConsole([]byte(fileContent), nil)
			if len(badlines) > 0 {
				for _, badline := range badlines {
					if warnOnly {
						plog.Errorf("bad log line detected: %v", badline)
					} else {
						plog.Warningf("bad log line detected: %v", badline)
					}
				}
				if !warnOnly {
					err = fmt.Errorf("errors found in log files")
					return elapsed, err
				}
			}
		}
	}
	return elapsed, err
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
