// Copyright 2015 CoreOS, Inc.
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

package kola

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/harness/reporters"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/platform"
	awsapi "github.com/coreos/mantle/platform/api/aws"
	azureapi "github.com/coreos/mantle/platform/api/azure"
	doapi "github.com/coreos/mantle/platform/api/do"
	esxapi "github.com/coreos/mantle/platform/api/esx"
	gcloudapi "github.com/coreos/mantle/platform/api/gcloud"
	openstackapi "github.com/coreos/mantle/platform/api/openstack"
	packetapi "github.com/coreos/mantle/platform/api/packet"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/aws"
	"github.com/coreos/mantle/platform/machine/azure"
	"github.com/coreos/mantle/platform/machine/do"
	"github.com/coreos/mantle/platform/machine/esx"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/coreos/mantle/platform/machine/openstack"
	"github.com/coreos/mantle/platform/machine/packet"
	"github.com/coreos/mantle/platform/machine/qemuiso"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/system"
)

// InstalledTestsDir is a directory where "installed" external
// can be placed; for example, a project like ostree can install
// tests at /usr/lib/coreos-assembler/tests/kola/ostree/...
// and this will be automatically picked up.
const InstalledTestsDir = "/usr/lib/coreos-assembler/tests/kola"
const InstalledTestMetaPrefix = "# kola:"

// InstalledTestDefaultTest is a special name; see the README-kola-ext.md
// for more information.
const InstalledTestDefaultTest = "test.sh"

// This is the same string from https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst
// Specifying this in the tags list is required to denote a need for Internet access
const NeedsInternetTag = "needs-internet"

// Don't e.g. check console for kernel errors, SELinux AVCs, etc.
const SkipBaseChecksTag = "skip-base-checks"

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	Options          = platform.Options{}
	AWSOptions       = awsapi.Options{Options: &Options}       // glue to set platform options from main
	AzureOptions     = azureapi.Options{Options: &Options}     // glue to set platform options from main
	DOOptions        = doapi.Options{Options: &Options}        // glue to set platform options from main
	ESXOptions       = esxapi.Options{Options: &Options}       // glue to set platform options from main
	GCEOptions       = gcloudapi.Options{Options: &Options}    // glue to set platform options from main
	OpenStackOptions = openstackapi.Options{Options: &Options} // glue to set platform options from main
	PacketOptions    = packetapi.Options{Options: &Options}    // glue to set platform options from main
	QEMUOptions      = unprivqemu.Options{Options: &Options}   // glue to set platform options from main
	QEMUIsoOptions   = qemuiso.Options{Options: &Options}      // glue to set platform options from main

	CosaBuild *sdk.LocalBuild // this is a parsed cosa build

	TestParallelism int    //glue var to set test parallelism from main
	TAPFile         string // if not "", write TAP results here
	NoNet           bool   // Disable tests requiring Internet

	DenylistedTests []string // tests which are on the denylist
	Tags            []string // tags to be ran

	consoleChecks = []struct {
		desc     string
		match    *regexp.Regexp
		skipFlag *register.Flag
	}{
		{
			desc:     "emergency shell",
			match:    regexp.MustCompile("Press Enter for emergency shell|Starting Emergency Shell|You are in emergency mode"),
			skipFlag: &[]register.Flag{register.NoEmergencyShellCheck}[0],
		},
		{
			desc:  "dracut fatal",
			match: regexp.MustCompile("dracut: Refusing to continue"),
		},
		{
			desc:  "kernel panic",
			match: regexp.MustCompile("Kernel panic - not syncing: (.*)"),
		},
		{
			desc:  "kernel oops",
			match: regexp.MustCompile("Oops:"),
		},
		{
			desc:  "kernel warning",
			match: regexp.MustCompile(`WARNING: CPU: \d+ PID: \d+ at (.+)`),
		},
		{
			desc:  "failure of disk under I/O",
			match: regexp.MustCompile("rejecting I/O to offline device"),
		},
		{
			// Failure to set up Packet networking in initramfs,
			// perhaps due to unresponsive metadata server
			desc:  "coreos-metadata failure to set up initramfs network",
			match: regexp.MustCompile("Failed to start CoreOS Static Network Agent"),
		},
		{
			// https://github.com/coreos/bugs/issues/2065
			desc:  "excessive bonding link status messages",
			match: regexp.MustCompile("(?s:link status up for interface [^,]+, enabling it in [0-9]+ ms.*?){10}"),
		},
		{
			// https://github.com/coreos/bugs/issues/2180
			desc:  "ext4 delayed allocation failure",
			match: regexp.MustCompile(`EXT4-fs \([^)]+\): Delayed block allocation failed for inode \d+ at logical offset \d+ with max blocks \d+ with (error \d+)`),
		},
		{
			// https://github.com/coreos/bugs/issues/2284
			desc:  "GRUB memory corruption",
			match: regexp.MustCompile("((alloc|free) magic) (is )?broken"),
		},
		{
			// https://github.com/coreos/bugs/issues/2435
			desc:  "Ignition fetch cancellation race",
			match: regexp.MustCompile(`ignition\[[0-9]+\]: failed to fetch config: context canceled`),
		},
		{
			// https://github.com/coreos/bugs/issues/2526
			desc:  "initrd-cleanup.service terminated",
			match: regexp.MustCompile(`initrd-cleanup\.service: Main process exited, code=killed, status=15/TERM`),
		},
		{
			// kernel 4.14.11
			desc:  "bad page table",
			match: regexp.MustCompile(`mm/pgtable-generic.c:\d+: bad (p.d|pte)`),
		},
		{
			desc:  "Go panic",
			match: regexp.MustCompile("panic: (.*)"),
		},
		{
			desc:  "segfault",
			match: regexp.MustCompile("SIGSEGV|=11/SEGV"),
		},
		{
			desc:  "core dump",
			match: regexp.MustCompile("[Cc]ore dump"),
		},
		{
			desc:  "systemd ordering cycle",
			match: regexp.MustCompile("Ordering cycle found"),
		},
	}
)

const (
	// kolaExtBinDataDir is where data will be stored on the target (but use the environment variable)
	kolaExtBinDataDir = "/var/opt/kola/extdata"

	// kolaExtBinDataEnv is an environment variable pointing to the above
	kolaExtBinDataEnv = "KOLA_EXT_DATA"

	// kolaExtBinDataName is the name for test dependency data
	kolaExtBinDataName = "data"
)

// KoletResult is serialized JSON passed from kolet to the harness
type KoletResult struct {
	Reboot string
}

const KoletExtTestUnit = "kola-runext.service"
const KoletRebootAckFifo = "/run/kolet-reboot-ack"

// NativeRunner is a closure passed to all kola test functions and used
// to run native go functions directly on kola machines. It is necessary
// glue until kola does introspection.
type NativeRunner func(funcName string, m platform.Machine) error

func NewFlight(pltfrm string) (flight platform.Flight, err error) {
	switch pltfrm {
	case "aws":
		flight, err = aws.NewFlight(&AWSOptions)
	case "azure":
		flight, err = azure.NewFlight(&AzureOptions)
	case "do":
		flight, err = do.NewFlight(&DOOptions)
	case "esx":
		flight, err = esx.NewFlight(&ESXOptions)
	case "gce":
		flight, err = gcloud.NewFlight(&GCEOptions)
	case "openstack":
		flight, err = openstack.NewFlight(&OpenStackOptions)
	case "packet":
		flight, err = packet.NewFlight(&PacketOptions)
	case "qemu-unpriv":
		flight, err = unprivqemu.NewFlight(&QEMUOptions)
	case "qemu-iso":
		flight, err = qemuiso.NewFlight(&QEMUIsoOptions)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}
	return
}

// matchesPatterns returns true if `s` matches one of the patterns in `patterns`.
func matchesPatterns(s string, patterns []string) (bool, error) {
	for _, pattern := range patterns {
		match, err := filepath.Match(pattern, s)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

// hasString returns true if `s` equals one of the strings in `slice`.
func hasString(s string, slice []string) bool {
	for _, e := range slice {
		if e == s {
			return true
		}
	}
	return false
}

func testRequiresInternet(test *register.Test) bool {
	for _, flag := range test.Flags {
		if flag == register.RequiresInternetAccess {
			return true
		}
	}
	// Also parse the newer tag for this
	for _, tag := range test.Tags {
		if tag == NeedsInternetTag {
			return true
		}
	}
	return false
}

func filterTests(tests map[string]*register.Test, patterns []string, pltfrm string) (map[string]*register.Test, error) {
	r := make(map[string]*register.Test)

	checkPlatforms := []string{pltfrm}

	// qemu-unpriv has the same restrictions as QEMU but might also want additional restrictions due to the lack of a Local cluster
	if pltfrm == "qemu-unpriv" {
		checkPlatforms = append(checkPlatforms, "qemu")
	}

	noPattern := hasString("*", patterns)
	for name, t := range tests {
		if NoNet && testRequiresInternet(t) {
			plog.Debugf("Skipping test that requires network: %s", t.Name)
			continue
		}

		var denylisted bool
		// Drop anything which is denylisted directly or by pattern
		for _, bl := range DenylistedTests {
			match, err := filepath.Match(bl, t.Name)
			if err != nil {
				return nil, err
			}
			// If it matched the pattern this test is denylisted
			if match {
				denylisted = true
				break
			}

			// Check if any native tests are denylisted. To exclude native tests, specify the high level
			// test and a "/" and then the glob pattern.
			// - basic/TestNetworkScripts: excludes only TestNetworkScripts
			// - basic/* - excludes all
			// - If no pattern is specified after / , excludes none
			nativedenylistindex := strings.Index(bl, "/")
			if nativedenylistindex > -1 {
				// Check native tests for arch specific exclusion
				for nativetestname := range t.NativeFuncs {
					match, err := filepath.Match(bl[nativedenylistindex+1:], nativetestname)
					if err != nil {
						return nil, err
					}
					if match {
						delete(t.NativeFuncs, nativetestname)
					}
				}
			}
		}
		// If the test is denylisted, skip it and continue to the next test
		if denylisted {
			plog.Debugf("Skipping denylisted test %s", t.Name)
			continue
		}

		match, err := matchesPatterns(t.Name, patterns)
		if err != nil {
			return nil, err
		}

		tagMatch := false
		for _, tag := range Tags {
			tagMatch = hasString(tag, t.Tags)
			if tagMatch {
				break
			}
		}

		if (!noPattern && !match && !tagMatch) || (!tagMatch && noPattern && len(Tags) > 0) {
			continue
		}

		isAllowed := func(item string, include, exclude []string) (bool, bool) {
			allowed, excluded := true, false
			for _, i := range include {
				if i == item {
					allowed = true
					break
				} else {
					allowed = false
				}
			}
			for _, i := range exclude {
				if i == item {
					allowed = false
					excluded = true
				}
			}
			return allowed, excluded
		}

		isExcluded := false
		allowed := false
		for _, platform := range checkPlatforms {
			allowedPlatform, excluded := isAllowed(platform, t.Platforms, t.ExcludePlatforms)
			if excluded {
				isExcluded = true
				break
			}
			allowedArchitecture, _ := isAllowed(system.RpmArch(), t.Architectures, t.ExcludeArchitectures)
			allowed = allowed || (allowedPlatform && allowedArchitecture)
		}
		if isExcluded || !allowed {
			continue
		}

		if allowed, excluded := isAllowed(Options.Distribution, t.Distros, t.ExcludeDistros); !allowed || excluded {
			continue
		}
		if pltfrm == "qemu-unpriv" {
			if allowed, excluded := isAllowed(QEMUOptions.Firmware, t.Firmwares, t.ExcludeFirmwares); !allowed || excluded {
				continue
			}
		}

		// Check native tests for arch-specific and distro-specfic exclusion
		for k, NativeFuncWrap := range t.NativeFuncs {
			_, excluded := isAllowed(Options.Distribution, nil, NativeFuncWrap.Exclusions)
			if excluded {
				delete(t.NativeFuncs, k)
				continue
			}
			_, excluded = isAllowed(system.RpmArch(), nil, NativeFuncWrap.Exclusions)
			if excluded {
				delete(t.NativeFuncs, k)
			}
		}

		r[name] = t
	}

	return r, nil
}

// runProvidedTests is a harness for running multiple tests in parallel.
// Filters tests based on a glob pattern and by platform. Has access to all
// tests either registered in this package or by imported packages that
// register tests in their init() function.  outputDir is where various test
// logs and data will be written for analysis after the test run. If it already
// exists it will be erased!
func runProvidedTests(tests map[string]*register.Test, patterns []string, multiply int, pltfrm, outputDir string, propagateTestErrors bool) error {
	var versionStr string

	// Avoid incurring cost of starting machine in getClusterSemver when
	// either:
	// 1) none of the selected tests care about the version
	// 2) glob is an exact match which means minVersion will be ignored
	//    either way
	tests, err := filterTests(tests, patterns, pltfrm)
	if err != nil {
		plog.Fatal(err)
	}

	if multiply > 1 {
		newTests := make(map[string]*register.Test)
		for name, t := range tests {
			delete(register.Tests, name)
			for i := 0; i < multiply; i++ {
				newName := fmt.Sprintf("%s%d", name, i)
				newT := *t
				newT.Name = newName
				newTests[newName] = &newT
				register.RegisterTest(&newT)
			}
		}
		tests = newTests
	}

	flight, err := NewFlight(pltfrm)
	if err != nil {
		plog.Fatalf("Flight failed: %v", err)
	}
	defer flight.Destroy()

	opts := harness.Options{
		OutputDir: outputDir,
		Parallel:  TestParallelism,
		Verbose:   true,
		Reporters: reporters.Reporters{
			reporters.NewJSONReporter("report.json", pltfrm, versionStr),
		},
	}
	var htests harness.Tests
	for _, test := range tests {
		test := test // for the closure
		run := func(h *harness.H) {
			runTest(h, test, pltfrm, flight)
		}
		htests.Add(test.Name, run)
	}

	suite := harness.NewSuite(opts, htests)
	err = suite.Run()
	caughtTestError := err != nil
	if !propagateTestErrors {
		err = nil
	}

	if TAPFile != "" {
		src := filepath.Join(outputDir, "test.tap")
		if err2 := system.CopyRegularFile(src, TAPFile); err == nil && err2 != nil {
			err = err2
		}
	}

	if caughtTestError {
		fmt.Printf("FAIL, output in %v\n", outputDir)
	} else {
		fmt.Printf("PASS, output in %v\n", outputDir)
	}

	return err
}

func RunTests(patterns []string, multiply int, pltfrm, outputDir string, propagateTestErrors bool) error {
	return runProvidedTests(register.Tests, patterns, multiply, pltfrm, outputDir, propagateTestErrors)
}

func RunUpgradeTests(patterns []string, pltfrm, outputDir string, propagateTestErrors bool) error {
	return runProvidedTests(register.UpgradeTests, patterns, 0, pltfrm, outputDir, propagateTestErrors)
}

// externalTestMeta is parsed from kola.json in external tests
type externalTestMeta struct {
	Architectures   string   `json:"architectures,omitempty"`
	Platforms       string   `json:"platforms,omitempty"`
	Distros         string   `json:"distros,omitempty"`
	Tags            string   `json:"tags,omitempty"`
	AdditionalDisks []string `json:"additionalDisks,omitempty"`
	MinMemory       int      `json:"minMemory,omitempty"`
}

// metadataFromTestBinary extracts JSON-in-comment like:
// #!/bin/bash
// # kola: { "tags": ["ignition"], "architectures": ["x86_64"] }
// <test code here>
func metadataFromTestBinary(executable string) (*externalTestMeta, error) {
	f, err := os.Open(executable)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, 8192))
	var meta *externalTestMeta
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(line, InstalledTestMetaPrefix) {
			continue
		}
		buf := strings.TrimSpace(line[len(InstalledTestMetaPrefix):])
		dec := json.NewDecoder(strings.NewReader(buf))
		dec.DisallowUnknownFields()
		meta = &externalTestMeta{}
		if err := dec.Decode(meta); err != nil {
			return nil, errors.Wrapf(err, "parsing %s", line)
		}
		break
	}
	return meta, nil
}

// runExternalTest is an implementation of the "external" test framework.
// See README-kola-ext.md as well as the comments in kolet.go for reboot
// handling.
func runExternalTest(c cluster.TestCluster, mach platform.Machine) error {
	var previousRebootState string
	var stdout []byte
	var stderr []byte
	for {
		bootID, err := platform.GetMachineBootId(mach)
		if err != nil {
			return errors.Wrapf(err, "getting boot id")
		}
		plog.Debug("Starting kolet run-test-unit")
		if previousRebootState != "" {
			// quote around the value for systemd
			contents := fmt.Sprintf("AUTOPKGTEST_REBOOT_MARK='%s'", previousRebootState)
			plog.Debugf("Setting %s", contents)
			if err := platform.InstallFile(strings.NewReader(contents), mach, "/run/kola-runext-env"); err != nil {
				return err
			}
		}
		stdout, stderr, err = mach.SSH(fmt.Sprintf("sudo ./kolet run-test-unit %s", shellquote.Join(KoletExtTestUnit)))
		if err != nil {
			return errors.Wrapf(err, "kolet run-test-unit failed: %s", stderr)
		}
		koletRes := KoletResult{}
		if len(stdout) > 0 {
			err = json.Unmarshal(stdout, &koletRes)
			if err != nil {
				return errors.Wrapf(err, "parsing kolet json %s", string(stdout))
			}
		}
		// If no  reboot is requested, we're done
		if koletRes.Reboot == "" {
			return nil
		}

		// A reboot is requested
		previousRebootState = koletRes.Reboot
		plog.Debugf("Reboot request with mark='%s'", previousRebootState)
		// This signals to the subject that we have saved the mark, and the subject
		// can proceed with rebooting.  We stop sshd to ensure that the wait below
		// doesn't log in while ssh is shutting down.
		_, _, err = mach.SSH(fmt.Sprintf("sudo /bin/sh -c 'systemctl stop sshd && echo > %s'", KoletRebootAckFifo))
		if err != nil {
			return errors.Wrapf(err, "failed to acknowledge reboot")
		}
		plog.Debug("Waiting for reboot")
		err = mach.WaitForReboot(120*time.Second, bootID)
		if err != nil {
			return errors.Wrapf(err, "Waiting for reboot")
		}
		plog.Debug("Reboot complete")
	}
}

func registerExternalTest(testname, executable, dependencydir, ignition string, baseMeta externalTestMeta) error {
	config, err := conf.Ignition(ignition).Render()
	if err != nil {
		return errors.Wrapf(err, "Parsing config.ign")
	}

	targetMeta, err := metadataFromTestBinary(executable)
	if err != nil {
		return errors.Wrapf(err, "Parsing metadata from %s", executable)
	}
	if targetMeta == nil {
		metaCopy := baseMeta
		targetMeta = &metaCopy
	}

	remotepath := fmt.Sprintf("/usr/local/bin/kola-runext-%s", filepath.Base(executable))
	// Note this isn't Type=oneshot because it's cleaner to support self-SIGTERM that way
	unit := fmt.Sprintf(`[Unit]
[Service]
RemainAfterExit=yes
EnvironmentFile=-/run/kola-runext-env
Environment=KOLA_UNIT=%s
Environment=%s=%s
ExecStart=%s
`, KoletExtTestUnit, kolaExtBinDataEnv, kolaExtBinDataDir, remotepath)
	config.AddSystemdUnit(KoletExtTestUnit, unit, conf.NoState)

	// Architectures using 64k pages use slightly more memory, ask for more than requested
	// to make sure that we don't run out of it. Currently ppc64le and aarch64 use 64k pages.
	switch system.RpmArch() {
	case "ppc64le", "aarch64":
		if targetMeta.MinMemory <= 4096 {
			targetMeta.MinMemory = targetMeta.MinMemory * 2
		}
	}

	t := &register.Test{
		Name:          testname,
		ClusterSize:   1, // Hardcoded for now
		ExternalTest:  executable,
		DependencyDir: dependencydir,
		Tags:          []string{"external"},

		AdditionalDisks: targetMeta.AdditionalDisks,
		MinMemory:       targetMeta.MinMemory,

		Run: func(c cluster.TestCluster) {
			mach := c.Machines()[0]
			plog.Debugf("Running kolet")

			err := runExternalTest(c, mach)
			if err != nil {
				out, stderr, suberr := mach.SSH(fmt.Sprintf("sudo systemctl status --lines=40 %s", shellquote.Join(KoletExtTestUnit)))
				if len(out) > 0 {
					fmt.Printf("systemctl status %s:\n%s\n", KoletExtTestUnit, string(out))
				} else {
					fmt.Printf("Fetching status failed: %v\n", suberr)
				}
				if Options.SSHOnTestFailure {
					plog.Errorf("dropping to shell: kolet failed: %v: %s", err, stderr)
					if err := platform.Manhole(mach); err != nil {
						plog.Errorf("failed to get terminal via ssh: %v", err)
					}
				}
				c.Fatalf(errors.Wrapf(err, "kolet failed: %s", stderr).Error())
			}
		},

		UserData: conf.Ignition(config.String()),
	}

	// To avoid doubling the duplication here with register.Test, we support
	// a ! prefix (inspired by systemd unit syntax), like:
	//
	// architectures: !ppc64le s390x
	// platforms: aws qemu
	if strings.HasPrefix(targetMeta.Architectures, "!") {
		t.ExcludeArchitectures = strings.Fields(targetMeta.Architectures[1:])
	} else {
		t.Architectures = strings.Fields(targetMeta.Architectures)
	}
	if strings.HasPrefix(targetMeta.Platforms, "!") {
		t.ExcludePlatforms = strings.Fields(targetMeta.Platforms[1:])
	} else {
		t.Platforms = strings.Fields(targetMeta.Platforms)
	}
	if strings.HasPrefix(targetMeta.Distros, "!") {
		t.ExcludeDistros = strings.Fields(targetMeta.Distros[1:])
	} else {
		t.Distros = strings.Fields(targetMeta.Distros)
	}
	t.Tags = append(t.Tags, strings.Fields(targetMeta.Tags)...)
	// TODO validate tags here

	register.RegisterTest(t)

	return nil
}

// testIsDenyListed returns true if the test was denied on the CLI. This is
// used as an early filtering before the main filterTests function.
func testIsDenyListed(testname string) (bool, error) {
	for _, bl := range DenylistedTests {
		if match, err := filepath.Match(bl, testname); err != nil {
			return false, err
		} else if match {
			return true, nil
		}
	}
	return false, nil
}

// registerTestDir parses one test directory and registers it as a test
func registerTestDir(dir, testprefix string, children []os.FileInfo) error {
	var dependencydir string
	var meta externalTestMeta
	var err error
	ignition := `{ "ignition": { "version": "3.0.0" } }`
	executables := []string{}
	for _, c := range children {
		fpath := filepath.Join(dir, c.Name())
		// follow symlinks; oddly, there's no IsSymlink()
		if c.Mode()&os.ModeSymlink != 0 {
			c, err = os.Stat(fpath)
			if err != nil {
				return errors.Wrapf(err, "stat %s", fpath)
			}
		}
		isreg := c.Mode().IsRegular()
		if isreg && (c.Mode().Perm()&0001) > 0 {
			executables = append(executables, filepath.Join(dir, c.Name()))
		} else if isreg && c.Name() == "config.ign" {
			v, err := ioutil.ReadFile(filepath.Join(dir, c.Name()))
			if err != nil {
				return errors.Wrapf(err, "reading %s", c.Name())
			}
			ignition = string(v)
		} else if isreg && (c.Name() == "config.bu" || c.Name() == "config.fcc") {
			b, err := exec.Command("fcct", fpath).Output()
			if err != nil {
				return errors.Wrapf(err, "failed to fcct %s", fpath)
			}
			ignition = string(b)
		} else if isreg && c.Name() == "kola.json" {
			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()
			dec := json.NewDecoder(f)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&meta); err != nil {
				return errors.Wrapf(err, "parsing %s", fpath)
			}
		} else if c.IsDir() && c.Name() == kolaExtBinDataName {
			dependencydir = filepath.Join(dir, c.Name())
		} else if c.Mode()&os.ModeSymlink != 0 && c.Name() == kolaExtBinDataName {
			target, err := filepath.EvalSymlinks(filepath.Join(dir, c.Name()))
			if err != nil {
				return err
			}
			dependencydir = target
		} else if c.IsDir() {
			subdir := filepath.Join(dir, c.Name())
			subchildren, err := ioutil.ReadDir(subdir)
			if err != nil {
				return err
			}
			subprefix := fmt.Sprintf("%s.%s", testprefix, c.Name())
			if err := registerTestDir(subdir, subprefix, subchildren); err != nil {
				return err
			}
		} else if isreg && (c.Mode().Perm()&0001) == 0 {
			file, err := os.Open(filepath.Join(dir, c.Name()))
			if err != nil {
				return errors.Wrapf(err, "opening %s", c.Name())
			}
			scanner := bufio.NewScanner(file)
			scanner.Scan()
			if strings.HasPrefix(scanner.Text(), "#!") {
				plog.Warningf("Found non-executable file with shebang: %s\n", c.Name())
			}
		}
	}

	for _, executable := range executables {
		testname := testprefix
		if len(executables) > 1 || filepath.Base(executable) != InstalledTestDefaultTest {
			testname = fmt.Sprintf("%s.%s", testname, filepath.Base(executable))
		}

		// don't even register the test if it's denied; this allows us to avoid
		// erroring on Ignition config versions which we can't parse
		if denied, err := testIsDenyListed(testname); err != nil {
			return err
		} else if denied {
			plog.Debugf("Skipping denylisted external test %s", testname)
			continue
		}

		err := registerExternalTest(testname, executable, dependencydir, ignition, meta)
		if err != nil {
			return err
		}
	}

	return nil
}

func RegisterExternalTestsWithPrefix(dir, prefix string) error {
	testsdir := filepath.Join(dir, "tests/kola")
	children, err := ioutil.ReadDir(testsdir)
	if err != nil {
		if os.IsNotExist(err) {
			// The directory doesn't exist.. Skip registering tests
			return nil
		} else {
			return errors.Wrapf(err, "reading %s", dir)
		}
	}

	if err := registerTestDir(testsdir, prefix, children); err != nil {
		return err
	}

	return nil
}

// RegisterExternalTests iterates over a directory, and finds subdirectories
// that have exactly one executable binary.
func RegisterExternalTests(dir string) error {
	basename := fmt.Sprintf("ext.%s", filepath.Base(dir))
	return RegisterExternalTestsWithPrefix(dir, basename)
}

// runTest is a harness for running a single test.
// outputDir is where various test logs and data will be written for
// analysis after the test run. It should already exist.
func runTest(h *harness.H, t *register.Test, pltfrm string, flight platform.Flight) {
	h.Parallel()

	rconf := &platform.RuntimeConfig{
		OutputDir:          h.OutputDir(),
		NoSSHKeyInUserData: t.HasFlag(register.NoSSHKeyInUserData),
		NoSSHKeyInMetadata: t.HasFlag(register.NoSSHKeyInMetadata),
		InternetAccess:     testRequiresInternet(t),
	}
	c, err := flight.NewCluster(rconf)
	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		c.Destroy()
		for _, k := range t.Tags {
			if k == SkipBaseChecksTag {
				plog.Debugf("Skipping base checks for %s", t.Name)
				return
			}
		}
		for id, output := range c.ConsoleOutput() {
			for _, badness := range CheckConsole([]byte(output), t) {
				h.Errorf("Found %s on machine %s console", badness, id)
			}
		}
		for id, output := range c.JournalOutput() {
			for _, badness := range CheckConsole([]byte(output), t) {
				h.Errorf("Found %s on machine %s journal", badness, id)
			}
		}
	}()

	if t.ClusterSize > 0 {
		var userdata *conf.UserData = t.UserData

		options := platform.MachineOptions{
			AdditionalDisks: t.AdditionalDisks,
			MinMemory:       t.MinMemory,
		}
		if _, err := platform.NewMachines(c, userdata, t.ClusterSize, options); err != nil {
			h.Fatalf("Cluster failed starting machines: %v", err)
		}
	}

	// pass along all registered native functions
	var names []string
	for k := range t.NativeFuncs {
		names = append(names, k)
	}

	// Cluster -> TestCluster
	tcluster := cluster.TestCluster{
		H:           h,
		Cluster:     c,
		NativeFuncs: names,
		FailFast:    t.FailFast,
	}

	// drop kolet binary on machines
	if t.ExternalTest != "" || t.NativeFuncs != nil {
		if err := scpKolet(tcluster.Machines()); err != nil {
			h.Fatal(err)
		}
	}

	if t.ExternalTest != "" {
		in, err := os.Open(t.ExternalTest)
		if err != nil {
			h.Fatal(err)
		}
		defer in.Close()
		for _, mach := range tcluster.Machines() {
			unit := fmt.Sprintf("kola-runext-%s", filepath.Base(t.ExternalTest))
			remotepath := fmt.Sprintf("/usr/local/bin/%s", unit)
			if err := platform.InstallFile(in, mach, remotepath); err != nil {
				h.Fatal(errors.Wrapf(err, "uploading %s", t.ExternalTest))
			}
			defer func(mach platform.Machine) {
				unit := unit
				tcluster := tcluster
				path := filepath.Join(mach.RuntimeConf().OutputDir, mach.ID(), fmt.Sprintf("%s.txt", unit))
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
				if err != nil {
					h.Fatal(errors.Wrapf(err, "opening %s", path))
					return
				}
				defer f.Close()
				out := tcluster.MustSSHf(mach, "journalctl -t %s", unit)
				if _, err = f.WriteString(string(out)); err != nil {
					h.Errorf("failed to write journal: %v", err)
				}
			}(mach)
		}
	}

	if t.DependencyDir != "" {
		for _, mach := range tcluster.Machines() {
			if err := platform.CopyDirToMachine(t.DependencyDir, mach, kolaExtBinDataDir); err != nil {
				h.Fatal(errors.Wrapf(err, "copying dependencies %s to %s", t.DependencyDir, mach.ID()))
			}
		}
	}

	defer func() {
		// give some time for the remote journal to be flushed so it can be read
		// before we run the deferred machine destruction
		time.Sleep(2 * time.Second)
	}()

	// run test
	t.Run(tcluster)
}

// scpKolet searches for a kolet binary and copies it to the machine.
func scpKolet(machines []platform.Machine) error {
	mArch := system.RpmArch()
	for _, d := range []string{
		".",
		filepath.Dir(os.Args[0]),
		filepath.Join(filepath.Dir(os.Args[0]), mArch),
		filepath.Join("/usr/lib/kola", mArch),
	} {
		kolet := filepath.Join(d, "kolet")
		if _, err := os.Stat(kolet); err == nil {
			if err := cluster.DropLabeledFile(machines, kolet, "bin_t"); err != nil {
				return errors.Wrapf(err, "dropping kolet binary")
			}
			return nil
		}
	}
	return fmt.Errorf("Unable to locate kolet binary for %s", mArch)
}

// CheckConsole checks some console output for badness and returns short
// descriptions of any badness it finds. If t is specified, its flags are
// respected.
func CheckConsole(output []byte, t *register.Test) []string {
	var ret []string
	for _, check := range consoleChecks {
		if check.skipFlag != nil && t != nil && t.HasFlag(*check.skipFlag) {
			continue
		}
		match := check.match.FindSubmatch(output)
		if match != nil {
			badness := check.desc
			if len(match) > 1 {
				// include first subexpression
				badness += fmt.Sprintf(" (%s)", match[1])
			}
			ret = append(ret, badness)
		}
	}
	return ret
}

func SetupOutputDir(outputDir, platform string) (string, error) {
	defaulted := outputDir == ""

	var defaultBaseDirName string
	if defaulted && Options.CosaWorkdir != "" {
		defaultBaseDirName = filepath.Join(Options.CosaWorkdir, "tmp/kola")
	} else {
		defaultBaseDirName = "_kola_temp"
	}
	defaultDirName := fmt.Sprintf("%s-%s-%d", platform, time.Now().Format("2006-01-02-1504"), os.Getpid())

	if defaulted {
		if _, err := os.Stat(defaultBaseDirName); os.IsNotExist(err) {
			if err := os.Mkdir(defaultBaseDirName, 0777); err != nil {
				return "", err
			}
		}
		outputDir = filepath.Join(defaultBaseDirName, defaultDirName)
		// FIXME pass this down better than global state
		network.DefaultSSHDir = defaultBaseDirName
	}

	outputDir, err := harness.CleanOutputDir(outputDir)
	if err != nil {
		return "", err
	}

	if defaulted {
		tempLinkPath := filepath.Join(outputDir, "latest")
		linkPath := filepath.Join(defaultBaseDirName, platform+"-latest")
		// don't clobber existing files that are not symlinks
		st, err := os.Lstat(linkPath)
		if err == nil && (st.Mode()&os.ModeType) != os.ModeSymlink {
			return "", fmt.Errorf("%v exists and is not a symlink", linkPath)
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Symlink(defaultDirName, tempLinkPath); err != nil {
			return "", err
		}
		// atomic rename
		if err := os.Rename(tempLinkPath, linkPath); err != nil {
			os.Remove(tempLinkPath)
			return "", err
		}
	}

	return outputDir, nil
}
