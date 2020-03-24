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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"

	ignv3 "github.com/coreos/ignition/v2/config/v3_0"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/harness/reporters"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
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
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
)

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

	CosaBuild *sdk.LocalBuild // this is a parsed cosa build

	TestParallelism int    //glue var to set test parallelism from main
	TAPFile         string // if not "", write TAP results here

	BlacklistedTests []string // tests which are blacklisted

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
			match: regexp.MustCompile("ignition\\[[0-9]+\\]: failed to fetch config: context canceled"),
		},
		{
			// https://github.com/coreos/bugs/issues/2526
			desc:  "initrd-cleanup.service terminated",
			match: regexp.MustCompile("initrd-cleanup\\.service: Main process exited, code=killed, status=15/TERM"),
		},
		{
			// kernel 4.14.11
			desc:  "bad page table",
			match: regexp.MustCompile("mm/pgtable-generic.c:\\d+: bad (p.d|pte)"),
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

func filterTests(tests map[string]*register.Test, patterns []string, pltfrm string, version semver.Version) (map[string]*register.Test, error) {
	r := make(map[string]*register.Test)

	checkPlatforms := []string{pltfrm}

	// qemu-unpriv has the same restrictions as QEMU but might also want additional restrictions due to the lack of a Local cluster
	if pltfrm == "qemu-unpriv" {
		checkPlatforms = append(checkPlatforms, "qemu")
	}

	var blacklisted bool
	for name, t := range tests {
		// Drop anything which is blacklisted directly or by pattern
		blacklisted = false
		for _, bl := range BlacklistedTests {
			match, err := filepath.Match(bl, t.Name)
			if err != nil {
				return nil, err
			}
			// If it matched the pattern this test is blacklisted
			if match {
				blacklisted = true
				break
			}

			// Check if any native tests are blacklisted. To exclude native tests, specify the high level
			// test and a "/" and then the glob pattern.
			// - basic/TestNetworkScripts: excludes only TestNetworkScripts
			// - basic/* - excludes all
			// - If no pattern is specified after / , excludes none
			nativeblacklistindex := strings.Index(bl, "/")
			if nativeblacklistindex > -1 {
				// Check native tests for arch specific exclusion
				for nativetestname := range t.NativeFuncs {
					match, err := filepath.Match(bl[nativeblacklistindex+1:], nativetestname)
					if err != nil {
						return nil, err
					}
					if match {
						delete(t.NativeFuncs, nativetestname)
					}
				}
			}
		}
		// If the test is blacklisted, skip it and continue to the next test
		if blacklisted {
			plog.Debugf("Skipping blacklisted test %s", t.Name)
			continue
		}

		match, err := matchesPatterns(t.Name, patterns)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}

		// Check the test's min and end versions when running more than one test
		if !hasString(t.Name, patterns) && versionOutsideRange(version, t.MinVersion, t.EndVersion) {
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

		// Check native tests for arch specific exclusion
		for k, NativeFuncWrap := range t.NativeFuncs {
			_, excluded := isAllowed(system.RpmArch(), nil, NativeFuncWrap.ExcludeArchitectures)
			if excluded {
				delete(t.NativeFuncs, k)
			}
		}

		r[name] = t
	}

	return r, nil
}

// versionOutsideRange checks to see if version is outside [min, end). If end
// is a zero value, it is ignored and there is no upper bound. If version is a
// zero value, the bounds are ignored.
func versionOutsideRange(version, minVersion, endVersion semver.Version) bool {
	if version == (semver.Version{}) {
		return false
	}

	if version.LessThan(minVersion) {
		return true
	}

	if (endVersion != semver.Version{}) && !version.LessThan(endVersion) {
		return true
	}

	return false
}

// runProvidedTests is a harness for running multiple tests in parallel.
// Filters tests based on a glob pattern and by platform. Has access to all
// tests either registered in this package or by imported packages that
// register tests in their init() function.  outputDir is where various test
// logs and data will be written for analysis after the test run. If it already
// exists it will be erased!
func runProvidedTests(tests map[string]*register.Test, patterns []string, pltfrm, outputDir string, propagateTestErrors bool) error {
	var versionStr string

	// Avoid incurring cost of starting machine in getClusterSemver when
	// either:
	// 1) none of the selected tests care about the version
	// 2) glob is an exact match which means minVersion will be ignored
	//    either way
	tests, err := filterTests(tests, patterns, pltfrm, semver.Version{})
	if err != nil {
		plog.Fatal(err)
	}

	skipGetVersion := true
	for name, t := range tests {
		if !hasString(name, patterns) && (t.MinVersion != semver.Version{} || t.EndVersion != semver.Version{}) {
			skipGetVersion = false
			break
		}
	}

	flight, err := NewFlight(pltfrm)
	if err != nil {
		plog.Fatalf("Flight failed: %v", err)
	}
	defer flight.Destroy()

	if !skipGetVersion {
		plog.Info("Creating cluster to check semver...")
		version, err := getClusterSemver(flight, outputDir)
		if err != nil {
			plog.Fatal(err)
		}

		versionStr = version.String()

		// one more filter pass now that we know real version
		tests, err = filterTests(tests, patterns, pltfrm, *version)
		if err != nil {
			plog.Fatal(err)
		}
	}

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

func RunTests(patterns []string, pltfrm, outputDir string, propagateTestErrors bool) error {
	return runProvidedTests(register.Tests, patterns, pltfrm, outputDir, propagateTestErrors)
}

func RunUpgradeTests(patterns []string, pltfrm, outputDir string, propagateTestErrors bool) error {
	return runProvidedTests(register.UpgradeTests, patterns, pltfrm, outputDir, propagateTestErrors)
}

// externalTestMeta is parsed from kola.yaml in external tests
type externalTestMeta struct {
	Architectures string `json:",architectures,omitempty"`
	Platforms     string `json:",platforms,omitempty"`
}

func registerExternalTest(testname, executable, dependencydir, ignition string, meta externalTestMeta) error {
	ignc3, _, err := ignv3.Parse([]byte(ignition))
	if err != nil {
		return errors.Wrapf(err, "Parsing config.ign")
	}

	unitname := "kola-runext.service"
	remotepath := fmt.Sprintf("/usr/local/bin/kola-runext-%s", filepath.Base(executable))
	// Note this isn't Type=oneshot because it's cleaner to support self-SIGTERM that way
	unit := fmt.Sprintf(`[Unit]
[Service]
RemainAfterExit=yes
Environment=%s=%s
ExecStart=%s
[Install]
RequiredBy=multi-user.target
`, kolaExtBinDataEnv, kolaExtBinDataDir, remotepath)
	runextconfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     unitname,
					Contents: &unit,
					Enabled:  util.BoolToPtr(false),
				},
			},
		},
	}

	finalIgn := ignv3.Merge(ignc3, runextconfig)
	serializedIgn, err := json.Marshal(finalIgn)
	if err != nil {
		return errors.Wrapf(err, "serializing ignition")
	}

	t := &register.Test{
		Name:          testname,
		ClusterSize:   1, // Hardcoded for now
		ExternalTest:  executable,
		DependencyDir: dependencydir,

		Run: func(c cluster.TestCluster) {
			mach := c.Machines()[0]
			plog.Debugf("Running kolet")

			var stderr []byte
			var err error
			for {
				plog.Debug("Starting kolet run-test-unit")
				_, stderr, err = mach.SSH(fmt.Sprintf("sudo ./kolet run-test-unit %s", shellquote.Join(unitname)))
				if exit, ok := err.(*ssh.ExitError); ok {
					plog.Debug("Caught ssh.ExitError")
					// In the future I'd like to better support having the host reboot itself and
					// we just detect it.
					if exit.Signal() == "TERM" {
						plog.Debug("Caught SIGTERM from kolet run-test-unit, rebooting machine")
						suberr := mach.Reboot()
						if suberr == nil {
							err = nil
							continue
						}
						plog.Debug("Propagating ssh.ExitError")
						err = suberr
					}
				}
				// Other errors, just bomb out for now
				break
			}
			if err != nil {
				out, _, suberr := mach.SSH(fmt.Sprintf("sudo systemctl status %s", shellquote.Join(unitname)))
				if len(out) > 0 {
					fmt.Printf("systemctl status %s:\n%s\n", unitname, string(out))
				} else {
					fmt.Printf("Fetching status failed: %v\n", suberr)
				}
				if Options.SSHOnTestFailure {
					plog.Errorf("dropping to shell: kolet failed: %v: %s", err, stderr)
					platform.Manhole(mach)
				}
				c.Fatalf(errors.Wrapf(err, "kolet failed: %s", stderr).Error())
			}
		},

		UserDataV3: conf.Ignition(string(serializedIgn)),
	}

	// To avoid doubling the duplication here with register.Test, we support
	// a ! prefix (inspired by systemd unit syntax), like:
	//
	// architectures: !ppc64le s390x
	// platforms: aws qemu
	if strings.HasPrefix(meta.Architectures, "!") {
		t.ExcludeArchitectures = strings.Fields(meta.Architectures[1:])
	} else {
		t.Architectures = strings.Fields(meta.Architectures)
	}
	if strings.HasPrefix(meta.Platforms, "!") {
		t.ExcludePlatforms = strings.Fields(meta.Platforms[1:])
	} else {
		t.Platforms = strings.Fields(meta.Platforms)
	}

	register.RegisterTest(t)

	return nil
}

// registerTestDir parses one test directory and registers it as a test
func registerTestDir(dir, testprefix string, children []os.FileInfo) error {
	var dependencydir string
	var meta externalTestMeta
	ignition := `{ "ignition": { "version": "3.0.0" } }`
	executables := []string{}
	for _, c := range children {
		fpath := filepath.Join(dir, c.Name())
		isreg := c.Mode().IsRegular()
		if isreg && (c.Mode().Perm()&0001) > 0 {
			executables = append(executables, filepath.Join(dir, c.Name()))
		} else if isreg && c.Name() == "config.ign" {
			v, err := ioutil.ReadFile(filepath.Join(dir, c.Name()))
			if err != nil {
				return errors.Wrapf(err, "reading %s", c.Name())
			}
			ignition = string(v)
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
		}
	}

	for _, executable := range executables {
		testname := testprefix
		if len(executables) > 1 {
			testname = fmt.Sprintf("%s.%s", testname, filepath.Base(executable))
		}
		err := registerExternalTest(testname, executable, dependencydir, ignition, meta)
		if err != nil {
			return err
		}
	}

	return nil
}

// RegisterExternalTests iterates over a directory, and finds subdirectories
// that have exactly one executable binary.
func RegisterExternalTests(dir string) error {
	// eval symlinks to turn e.g. src/config into fedora-coreos-config
	// for the test basename.
	realdir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	basename := fmt.Sprintf("ext.%s", filepath.Base(realdir))

	testsdir := filepath.Join(dir, "tests/kola")
	children, err := ioutil.ReadDir(testsdir)
	if err != nil {
		return errors.Wrapf(err, "reading %s", dir)
	}

	if err := registerTestDir(testsdir, basename, children); err != nil {
		return err
	}

	return nil
}

// getClusterSemVer returns the CoreOS semantic version via starting a
// machine and checking
func getClusterSemver(flight platform.Flight, outputDir string) (*semver.Version, error) {
	var err error

	testDir := filepath.Join(outputDir, "get_cluster_semver")
	if err := os.MkdirAll(testDir, 0777); err != nil {
		return nil, err
	}

	cluster, err := flight.NewCluster(&platform.RuntimeConfig{
		OutputDir: testDir,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "creating cluster for semver check")
	}
	defer cluster.Destroy()

	m, err := cluster.NewMachine(nil)
	if err != nil {
		return nil, errors.Wrapf(err, "creating new machine for semver check")
	}

	out, stderr, err := m.SSH("grep ^VERSION_ID= /etc/os-release")
	if err != nil {
		return nil, errors.Wrapf(err, "parsing /etc/os-release: %s", stderr)
	}
	ver := strings.Split(string(out), "=")[1]

	// TODO: add distro specific version handling
	switch Options.Distribution {
	case "cl":
		return parseCLVersion(ver)
	case "rhcos":
		return &semver.Version{}, nil
	}

	return nil, fmt.Errorf("no case to handle version parsing for distribution %q", Options.Distribution)
}

func parseCLVersion(input string) (*semver.Version, error) {
	version, err := semver.NewVersion(input)
	if err != nil {
		return nil, errors.Wrapf(err, "parsing os-release semver")
	}

	return version, nil
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
		NoEnableSelinux:    t.HasFlag(register.NoEnableSelinux),
	}
	c, err := flight.NewCluster(rconf)
	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		c.Destroy()
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
		var userdata *conf.UserData
		if Options.IgnitionVersion == "v2" && t.UserData != nil {
			userdata = t.UserData
		} else {
			userdata = t.UserDataV3
		}

		if _, err := platform.NewMachines(c, userdata, t.ClusterSize); err != nil {
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
		if err := scpKolet(tcluster.Machines(), system.RpmArch()); err != nil {
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
			remotepath := fmt.Sprintf("/usr/local/bin/kola-runext-%s", filepath.Base(t.ExternalTest))
			if err := platform.InstallFile(in, mach, remotepath); err != nil {
				h.Fatal(errors.Wrapf(err, "uploading %s", t.ExternalTest))
			}
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

// returns the arch part of an sdk board name
func boardToArch(board string) string {
	return strings.SplitN(board, "-", 2)[0]
}

// scpKolet searches for a kolet binary and copies it to the machine.
func scpKolet(machines []platform.Machine, mArch string) error {
	for _, d := range []string{
		".",
		filepath.Dir(os.Args[0]),
		filepath.Join(filepath.Dir(os.Args[0]), mArch),
		filepath.Join("/usr/lib/kola", mArch),
	} {
		kolet := filepath.Join(d, "kolet")
		if _, err := os.Stat(kolet); err == nil {
			if err := cluster.DropFile(machines, kolet); err != nil {
				return errors.Wrapf(err, "dropping kolet binary")
			}
			// If in the future we want to care about machines without SELinux, let's
			// do basically test -d /sys/fs/selinux or run `getenforce`.
			for _, machine := range machines {
				out, stderr, err := machine.SSH("sudo chcon -t bin_t kolet")
				if err != nil {
					return errors.Wrapf(err, "running chcon on kolet: %s: %s", out, stderr)
				}
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
	defaultBaseDirName := "_kola_temp"
	defaultDirName := fmt.Sprintf("%s-%s-%d", platform, time.Now().Format("2006-01-02-1504"), os.Getpid())

	if defaulted {
		if _, err := os.Stat(defaultBaseDirName); os.IsNotExist(err) {
			if err := os.Mkdir(defaultBaseDirName, 0777); err != nil {
				return "", err
			}
		}
		outputDir = filepath.Join(defaultBaseDirName, defaultDirName)
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
