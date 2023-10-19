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
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/coreos/coreos-assembler/mantle/harness"
	"github.com/coreos/coreos-assembler/mantle/harness/reporters"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/network"
	"github.com/coreos/coreos-assembler/mantle/platform"
	awsapi "github.com/coreos/coreos-assembler/mantle/platform/api/aws"
	azureapi "github.com/coreos/coreos-assembler/mantle/platform/api/azure"
	doapi "github.com/coreos/coreos-assembler/mantle/platform/api/do"
	esxapi "github.com/coreos/coreos-assembler/mantle/platform/api/esx"
	gcloudapi "github.com/coreos/coreos-assembler/mantle/platform/api/gcloud"
	openstackapi "github.com/coreos/coreos-assembler/mantle/platform/api/openstack"
	packetapi "github.com/coreos/coreos-assembler/mantle/platform/api/packet"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/aws"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/azure"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/do"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/esx"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/gcloud"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/openstack"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/packet"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemuiso"
	"github.com/coreos/coreos-assembler/mantle/system"
	"github.com/coreos/coreos-assembler/mantle/util"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
)

// InstalledTestsDir is a directory where "installed" external
// can be placed; for example, a project like ostree can install
// tests at /usr/lib/coreos-assembler/tests/kola/ostree/...
// and this will be automatically picked up.
const InstalledTestsDir = "/usr/lib/coreos-assembler/tests/kola"

// InstalledTestMetaPrefix is the prefix for JSON-formatted metadata
const InstalledTestMetaPrefix = "# kola:"

// InstalledTestMetaPrefixYaml is the prefix for YAML-formatted metadata
const InstalledTestMetaPrefixYaml = "## kola:"

// InstalledTestDefaultTest is a special name; see the README-kola-ext.md
// for more information.
const InstalledTestDefaultTest = "test.sh"

// This is the same string from https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst
// Specifying this in the tags list is required to denote a need for Internet access
const NeedsInternetTag = "needs-internet"

// PlatformIndependentTag is currently equivalent to platform: qemu, but that may change in the future.
// For more, see the doc in external-tests.md.
const PlatformIndependentTag = "platform-independent"

// The string for the tag that indicates a test has been marked as allowing rerun success.
// In some cases the internal test framework will add this tag to a test to indicate if
// the test passes a rerun to allow the run to succeed.
const AllowRerunSuccessTag = "allow-rerun-success"

// defaultPlatformIndependentPlatform is the platform where we run tests that claim platform independence
const defaultPlatformIndependentPlatform = "qemu"

// Don't e.g. check console for kernel errors, SELinux AVCs, etc.
const SkipBaseChecksTag = "skip-base-checks"

// Date format for snooze date specified in kola-denylist.yaml (YYYY-MM-DD)
const snoozeFormat = "2006-01-02"

// SkipConsoleWarningsTag will cause kola not to check console for kernel errors.
// This overlaps with SkipBaseChecksTag above, but is really a special flag for kola-denylist.yaml.
const SkipConsoleWarningsTag = "skip-console-warnings"

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "kola")

	Options          = platform.Options{}
	AWSOptions       = awsapi.Options{Options: &Options}       // glue to set platform options from main
	AzureOptions     = azureapi.Options{Options: &Options}     // glue to set platform options from main
	DOOptions        = doapi.Options{Options: &Options}        // glue to set platform options from main
	ESXOptions       = esxapi.Options{Options: &Options}       // glue to set platform options from main
	GCPOptions       = gcloudapi.Options{Options: &Options}    // glue to set platform options from main
	OpenStackOptions = openstackapi.Options{Options: &Options} // glue to set platform options from main
	PacketOptions    = packetapi.Options{Options: &Options}    // glue to set platform options from main
	QEMUOptions      = qemu.Options{Options: &Options}         // glue to set platform options from main
	QEMUIsoOptions   = qemuiso.Options{Options: &Options}      // glue to set platform options from main

	CosaBuild *util.LocalBuild // this is a parsed cosa build

	TestParallelism int    //glue var to set test parallelism from main
	TAPFile         string // if not "", write TAP results here
	NoNet           bool   // Disable tests requiring Internet
	// ForceRunPlatformIndependent will cause tests that claim platform-independence to run
	ForceRunPlatformIndependent bool

	// SkipConsoleWarnings is set via SkipConsoleWarningsTag in kola-denylist.yaml
	SkipConsoleWarnings bool
	DenylistedTests     []string // tests which are on the denylist
	WarnOnErrorTests    []string // denylisted tests we are going to run and warn in case of error
	Tags                []string // tags to be ran

	// Sharding is a string of the form: hash:m/n where m and n are integers to run only tests which hash to m.
	Sharding string

	extTestNum  = 1 // Assigns a unique number to each non-exclusive external test
	testResults protectedTestResults

	nonexclusivePrefixMatch  = regexp.MustCompile(`^non-exclusive-test-bucket-[0-9]/`)
	nonexclusiveWrapperMatch = regexp.MustCompile(`^non-exclusive-test-bucket-[0-9]$`)

	consoleChecks = []struct {
		desc              string
		match             *regexp.Regexp
		warnOnly          bool
		allowRerunSuccess bool
		skipFlag          *register.Flag
	}{
		{
			desc:              "emergency shell",
			match:             regexp.MustCompile("Press Enter for emergency shell|Starting Emergency Shell|You are in emergency mode"),
			warnOnly:          false,
			allowRerunSuccess: false,
			skipFlag:          &[]register.Flag{register.NoEmergencyShellCheck}[0],
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
			// For this one we see it sometimes when I/O is really slow, which is often
			// more of an indication of a problem with resources in our pipeline rather
			// than a problem with the software we are testing. We'll mark it as warnOnly
			// so it's non-fatal and also allow for a rerun of a test that goes on to
			// fail that had this problem to ultimately result in success.
			desc:              "kernel soft lockup",
			match:             regexp.MustCompile("watchdog: BUG: soft lockup - CPU"),
			warnOnly:          true,
			allowRerunSuccess: true,
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
		{
			desc:  "oom killer",
			match: regexp.MustCompile("invoked oom-killer"),
		},
		{
			// https://github.com/coreos/fedora-coreos-config/pull/1797
			desc:  "systemd generator failure",
			match: regexp.MustCompile(`(/.*/system-generators/.*) (failed with exit status|terminated by signal|failed due to unknown reason)`),
		},
	}

	ErrWarnOnTestFail = errors.New("A test marked as warn:true failed.")
)

const (
	// kolaExtBinDataDir is where data will be stored on the target (but use the environment variable)
	kolaExtBinDataDir = "/var/opt/kola/extdata"

	// kolaExtBinDataEnv is an environment variable pointing to the above
	kolaExtBinDataEnv = "KOLA_EXT_DATA"

	// kolaExtContainerDataEnv includes the path to the ostree base container image in oci-archive format.
	kolaExtContainerDataEnv = "KOLA_EXT_OSTREE_OCIARCHIVE"

	// kolaExtBinDataName is the name for test dependency data
	kolaExtBinDataName = "data"
)

// KoletResult is serialized JSON passed from kolet to the harness
type KoletResult struct {
	Reboot string
}

const KoletExtTestUnit = "kola-runext"
const KoletRebootAckFifo = "/run/kolet-reboot-ack"

// Records failed tests for reruns
type protectedTestResults struct {
	results []*harness.H
	mu      sync.RWMutex
}

func (p *protectedTestResults) add(h *harness.H) {
	p.mu.Lock()
	p.results = append(p.results, h)
	p.mu.Unlock()
}

func (p *protectedTestResults) getResults() []*harness.H {
	p.mu.RLock()
	temp := p.results
	p.mu.RUnlock()
	return temp
}

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
	case "gcp":
		flight, err = gcloud.NewFlight(&GCPOptions)
	case "openstack":
		flight, err = openstack.NewFlight(&OpenStackOptions)
	case "packet":
		flight, err = packet.NewFlight(&PacketOptions)
	case "qemu":
		flight, err = qemu.NewFlight(&QEMUOptions)
	case "qemu-iso":
		flight, err = qemuiso.NewFlight(&QEMUIsoOptions)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}
	return
}

// MatchesPatterns returns true if `s` matches one of the patterns in `patterns`.
func MatchesPatterns(s string, patterns []string) (bool, error) {
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

// HasString returns true if `s` equals one of the strings in `slice`.
func HasString(s string, slice []string) bool {
	for _, e := range slice {
		if e == s {
			return true
		}
	}
	return false
}

func testSkipBaseChecks(test *register.Test) bool {
	return HasString(SkipBaseChecksTag, test.Tags)
}

func testRequiresInternet(test *register.Test) bool {
	return HasString(NeedsInternetTag, test.Tags)
}

func markTestForRerunSuccess(test *register.Test, msg string) {
	if !HasString(AllowRerunSuccessTag, test.Tags) {
		plog.Warningf("%s Adding as candidate for rerun success: %s", msg, test.Name)
		test.Tags = append(test.Tags, AllowRerunSuccessTag)
	}
}

type DenyListObj struct {
	Pattern    string   `yaml:"pattern"`
	Tracker    string   `yaml:"tracker"`
	Streams    []string `yaml:"streams"`
	Arches     []string `yaml:"arches"`
	Platforms  []string `yaml:"platforms"`
	SnoozeDate string   `yaml:"snooze"`
	OsVersion  []string `yaml:"osversion"`
	Warn       bool     `yaml:"warn"`
}

type ManifestData struct {
	Variables struct {
		Stream    string `yaml:"stream"`
		OsVersion string `yaml:"osversion"`
	} `yaml:"variables"`
}

type InitConfigData struct {
	ConfigVariant string `json:"coreos-assembler.config-variant"`
}

func ParseDenyListYaml(pltfrm string) error {
	var objs []DenyListObj

	// Parse kola-denylist into structs
	pathToDenyList := filepath.Join(Options.CosaWorkdir, "src/config/kola-denylist.yaml")
	denyListFile, err := os.ReadFile(pathToDenyList)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	plog.Debug("Found kola-denylist.yaml. Processing listed denials.")
	err = yaml.Unmarshal(denyListFile, &objs)
	if err != nil {
		return err
	}

	plog.Debug("Parsed kola-denylist.yaml")

	// Look for the right manifest, taking into account the variant
	var manifest ManifestData
	var pathToManifest string
	pathToInitConfig := filepath.Join(Options.CosaWorkdir, "src/config.json")
	initConfigFile, err := os.ReadFile(pathToInitConfig)
	if os.IsNotExist(err) {
		// No variant config found. Let's read the default manifest
		pathToManifest = filepath.Join(Options.CosaWorkdir, "src/config/manifest.yaml")
	} else if err != nil {
		// Unexpected error
		return err
	} else {
		// Figure out the variant and read the corresponding manifests
		var initConfig InitConfigData
		err = json.Unmarshal(initConfigFile, &initConfig)
		if err != nil {
			return err
		}
		pathToManifest = filepath.Join(Options.CosaWorkdir, fmt.Sprintf("src/config/manifest-%s.yaml", initConfig.ConfigVariant))
	}
	manifestFile, err := os.ReadFile(pathToManifest)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(manifestFile, &manifest)
	if err != nil {
		return err
	}

	// Get the stream and osversion variables from the manifest
	stream := manifest.Variables.Stream
	osversion := manifest.Variables.OsVersion

	// Get the current arch & current time
	arch := Options.CosaBuildArch
	today := time.Now()

	plog.Debugf("Denylist: Skipping tests for stream: '%s', osversion: '%s', arch: '%s'\n", stream, osversion, arch)

	// Accumulate patterns filtering by set policies
	plog.Debug("Processing denial patterns from yaml...")
	for _, obj := range objs {
		if len(obj.Arches) > 0 && !HasString(arch, obj.Arches) {
			continue
		}

		if len(obj.Platforms) > 0 && !HasString(pltfrm, obj.Platforms) {
			continue
		}

		if len(stream) > 0 && len(obj.Streams) > 0 && !HasString(stream, obj.Streams) {
			continue
		}

		if len(osversion) > 0 && len(obj.OsVersion) > 0 && !HasString(osversion, obj.OsVersion) {
			continue
		}

		// Process "special" patterns which aren't test names, but influence overall behavior
		if obj.Pattern == SkipConsoleWarningsTag {
			SkipConsoleWarnings = true
			continue
		}

		if obj.SnoozeDate != "" {
			snoozeDate, err := time.Parse(snoozeFormat, obj.SnoozeDate)
			if err != nil {
				return err
			}
			if today.After(snoozeDate) {
				fmt.Printf("⏰ Snooze for kola test pattern \"%s\" expired on %s\n", obj.Pattern, snoozeDate.Format("Jan 02 2006"))
				if obj.Warn {
					fmt.Printf("⚠️  Will warn on failure for kola test pattern \"%s\":\n", obj.Pattern)
					WarnOnErrorTests = append(WarnOnErrorTests, obj.Pattern)
				}
			} else {
				fmt.Printf("🕒  Snoozing kola test pattern \"%s\" until %s\n", obj.Pattern, snoozeDate.Format("Jan 02 2006"))
				DenylistedTests = append(DenylistedTests, obj.Pattern)
			}
		} else {
			if obj.Warn {
				fmt.Printf("⚠️  Will warn on failure for kola test pattern \"%s\":\n", obj.Pattern)
				WarnOnErrorTests = append(WarnOnErrorTests, obj.Pattern)
			} else {
				fmt.Printf("⏭️  Skipping kola test pattern \"%s\":\n", obj.Pattern)
				DenylistedTests = append(DenylistedTests, obj.Pattern)
			}
		}
		if obj.Tracker != "" {
			fmt.Printf("  👉 %s\n", obj.Tracker)
		}
	}

	return nil
}

func filterTests(tests map[string]*register.Test, patterns []string, pltfrm string) (map[string]*register.Test, error) {
	r := make(map[string]*register.Test)

	checkPlatforms := []string{pltfrm}

	// sort tags into include/exclude
	positiveTags := []string{}
	negativeTags := []string{}
	for _, tag := range Tags {
		if strings.HasPrefix(tag, "!") {
			negativeTags = append(negativeTags, tag[1:])
		} else {
			positiveTags = append(positiveTags, tag)
		}
	}

	// Higher-level functions default to '*' if the user didn't pass anything.
	// Notice this. (This totally ignores the corner case where the user
	// actually typed '*').
	userTypedPattern := !HasString("*", patterns)
	for name, t := range tests {
		if NoNet && testRequiresInternet(t) {
			plog.Debugf("Skipping test that requires network: %s", t.Name)
			continue
		}

		nameMatch, err := MatchesPatterns(t.Name, patterns)
		if err != nil {
			return nil, err
		}

		tagMatch := false
		for _, tag := range positiveTags {
			tagMatch = HasString(tag, t.Tags) || tag == t.RequiredTag
			if tagMatch {
				break
			}
		}

		negativeTagMatch := false
		for _, tag := range negativeTags {
			negativeTagMatch = HasString(tag, t.Tags)
			if negativeTagMatch {
				break
			}
		}
		if negativeTagMatch {
			continue
		}

		if t.RequiredTag != "" && // if the test has a required tag...
			!HasString(t.RequiredTag, positiveTags) && // and that tag was not provided by the user...
			(!userTypedPattern || !nameMatch) { // and the user didn't request it by name...
			continue // then skip it
		}

		if userTypedPattern {
			// If the user explicitly typed a pattern, then the test *must*
			// match by name or by tag. Otherwise, we skip it.
			if !nameMatch && !tagMatch {
				continue
			}
		} else {
			// If the user didn't explicitly type a pattern, then normally we
			// accept all tests, but if they *did* specify tags, then we only
			// accept tests which match those tags.
			if len(positiveTags) > 0 && !tagMatch {
				continue
			}
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

		// For now, we hardcode platform independent tests to run only on one platform.
		// But in the future, we should optimize this so that an overall
		// test planner/scheduler knows to run the test at most once or twice.
		// Platform independent tests could also run on AWS sometimes for example.
		if !ForceRunPlatformIndependent {
			for _, tag := range t.Tags {
				if tag == PlatformIndependentTag {
					t.Platforms = []string{defaultPlatformIndependentPlatform}
				}
			}
		}

		isExcluded := false
		allowed := false
		for _, platform := range checkPlatforms {
			allowedPlatform, excluded := isAllowed(platform, t.Platforms, t.ExcludePlatforms)
			if excluded {
				isExcluded = true
				break
			}
			allowedArchitecture, _ := isAllowed(coreosarch.CurrentRpmArch(), t.Architectures, t.ExcludeArchitectures)
			allowed = allowed || (allowedPlatform && allowedArchitecture)
		}
		if isExcluded || !allowed {
			continue
		}

		if allowed, excluded := isAllowed(Options.Distribution, t.Distros, t.ExcludeDistros); !allowed || excluded {
			continue
		}
		if pltfrm == "qemu" {
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
			_, excluded = isAllowed(coreosarch.CurrentRpmArch(), nil, NativeFuncWrap.Exclusions)
			if excluded {
				delete(t.NativeFuncs, k)
			}
		}

		r[name] = t
	}

	return r, nil
}

func filterDenylistedTests(tests map[string]*register.Test) (map[string]*register.Test, error) {
	r := make(map[string]*register.Test)
	for name, t := range tests {
		denylisted := false
		// Detect anything which is denylisted directly or by pattern
		for _, bl := range DenylistedTests {
			nameMatch, err := filepath.Match(bl, t.Name)
			if err != nil {
				return nil, err
			}
			// If it matched the pattern this test is denylisted
			if nameMatch {
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
					nameMatch, err := filepath.Match(bl[nativedenylistindex+1:], nativetestname)
					if err != nil {
						return nil, err
					}
					if nameMatch {
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
func runProvidedTests(testsBank map[string]*register.Test, patterns []string, multiply int, rerun bool, rerunSuccessTags []string, pltfrm, outputDir string) error {
	var versionStr string

	// Avoid incurring cost of starting machine in getClusterSemver when
	// either:
	// 1) none of the selected tests care about the version
	// 2) glob is an exact match which means minVersion will be ignored
	//    either way

	// Add denylisted tests in kola-denylist.yaml to DenylistedTests
	err := ParseDenyListYaml(pltfrm)
	if err != nil {
		plog.Fatal(err)
	}

	// Make sure all given patterns by the user match at least one test
	for _, pattern := range patterns {
		match, err := patternMatchesTests(pattern, testsBank)
		if err != nil {
			plog.Fatal(err)
		}
		if !match {
			plog.Fatalf("The user provided pattern didn't match any tests: %s", pattern)
		}
	}

	tests, err := filterTests(testsBank, patterns, pltfrm)
	if err != nil {
		plog.Fatal(err)
	}

	if len(tests) == 0 {
		plog.Fatalf("There are no matching tests to run on this architecture/platform: %s %s", coreosarch.CurrentRpmArch(), pltfrm)
	}

	tests, err = filterDenylistedTests(tests)
	if err != nil {
		plog.Fatal(err)
	}

	if len(tests) == 0 {
		fmt.Printf("There are no tests to run because all tests are denylisted. Output in %v\n", outputDir)
		return nil
	}

	flight, err := NewFlight(pltfrm)
	if err != nil {
		plog.Fatalf("Flight failed: %v", err)
	}
	defer flight.Destroy()
	// Generate non-exclusive test wrapper (run multiple tests in one VM)
	var nonExclusiveTests []*register.Test
	for _, test := range tests {
		if test.NonExclusive {
			if test.ExternalTest == "" {
				plog.Fatalf("Tests compiled in kola must be exclusive: %v", test.Name)
			}
			nonExclusiveTests = append(nonExclusiveTests, test)
			delete(tests, test.Name)
		}
	}

	if len(nonExclusiveTests) == 1 {
		// If there is only one test then it can just be run by itself
		// so add it back to the tests map.
		tests[nonExclusiveTests[0].Name] = nonExclusiveTests[0]
	} else if len(nonExclusiveTests) > 0 {
		buckets := createTestBuckets(nonExclusiveTests)
		numBuckets := len(buckets)
		for i := 0; i < numBuckets; {
			// This test does not need to be registered since it is temporarily
			// created to be used as a wrapper
			nonExclusiveWrapper := makeNonExclusiveTest(i, buckets[i], flight)
			if flight.ConfigTooLarge(*nonExclusiveWrapper.UserData) {
				// Since the merged config size is too large, we will split the bucket into
				// two buckets
				numTests := len(buckets[i])
				if numTests == 1 {
					// This test bucket cannot be split further so the single test config
					// must be too large
					err = fmt.Errorf("test %v has a config that is too large", buckets[i][0].Name)
					plog.Fatal(err)
				}
				newBucket1 := buckets[i][:numTests/2]
				newBucket2 := buckets[i][numTests/2:]
				buckets[i] = newBucket1
				buckets = append(buckets, newBucket2)
				// Since we're adding a bucket we'll bump the numBuckets and not
				// bump `i` during this loop iteration because we want to run through
				// the check again for the current bucket which should now have half
				// the tests, but may still have a config that's too large.
				numBuckets++
			} else {
				tests[nonExclusiveWrapper.Name] = &nonExclusiveWrapper
				i++ // Move to the next bucket to evaluate
			}
		}
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

	tests, err = shardTests(tests, Sharding)
	if err != nil {
		plog.Fatalf("%v", err)
	}

	opts := harness.Options{
		OutputDir: outputDir,
		Parallel:  TestParallelism,
		Sharding:  Sharding,
		Verbose:   true,
		Reporters: reporters.Reporters{
			reporters.NewJSONReporter("report.json", pltfrm, versionStr),
		},
	}

	var htests harness.Tests
	for _, test := range tests {
		test := test // for the closure
		run := func(h *harness.H) {
			defer func() {
				// Keep track of failed tests for a rerun
				testResults.add(h)
			}()
			// We launch a seperate cluster for each kola test
			// At the end of the test, its cluster is destroyed
			runTest(h, test, pltfrm, flight)
		}
		htests.Add(test.Name, run, (test.Timeout*time.Duration(100+(Options.ExtendTimeoutPercent)))/100)
	}

	handleSuiteErrors := func(outputDir string, suiteErr error) error {
		caughtTestError := suiteErr != nil

		if TAPFile != "" {
			src := filepath.Join(outputDir, "test.tap")
			err := system.CopyRegularFile(src, TAPFile)
			if suiteErr == nil && err != nil {
				return err
			}
		}

		if caughtTestError {
			fmt.Printf("FAIL, output in %v\n", outputDir)
		} else {
			fmt.Printf("PASS, output in %v\n", outputDir)
		}

		return suiteErr
	}

	suite := harness.NewSuite(opts, htests)
	runErr := suite.Run()
	runErr = handleSuiteErrors(outputDir, runErr)

	detectedFailedWarnTrueTests := len(getWarnTrueFailedTests(testResults.getResults())) != 0

	testsToRerun := getRerunnable(testsBank, testResults.getResults())
	numFailedTests := len(testsToRerun)
	if len(testsToRerun) > 0 && rerun {
		newOutputDir := filepath.Join(outputDir, "rerun")
		fmt.Printf("\n\n======== Re-running failed tests (flake detection) ========\n\n")
		reRunErr := runProvidedTests(testsToRerun, []string{"*"}, multiply, false, rerunSuccessTags, pltfrm, newOutputDir)
		if reRunErr == nil && allTestsAllowRerunSuccess(testsToRerun, rerunSuccessTags) {
			runErr = nil       // reset to success since all tests allowed rerun success
			numFailedTests = 0 // zero out the tally of failed tests
		} else {
			runErr = reRunErr
		}

	}

	// Return ErrWarnOnTestFail when ONLY tests with warn:true feature failed
	if detectedFailedWarnTrueTests && numFailedTests == 0 {
		return ErrWarnOnTestFail
	} else {
		return runErr
	}
}

func getWarnTrueFailedTests(tests []*harness.H) []string {
	var warnTrueFailedTests []string
	for _, test := range tests {
		if !test.Failed() {
			continue
		}
		name := GetBaseTestName(test.Name())
		if name == "" {
			continue // skip non-exclusive test wrapper
		}
		if IsWarningOnFailure(name) {
			warnTrueFailedTests = append(warnTrueFailedTests, name)
		}
	}
	return warnTrueFailedTests
}

func allTestsAllowRerunSuccess(testsToRerun map[string]*register.Test, rerunSuccessTags []string) bool {
	// Always consider the special AllowRerunSuccessTag that is added
	// by the test harness in some failure scenarios.
	rerunSuccessTags = append(rerunSuccessTags, AllowRerunSuccessTag)
	// Build up a map of rerunSuccessTags so that we can easily check
	// if a given tag is in the map.
	rerunSuccessTagMap := make(map[string]bool)
	for _, tag := range rerunSuccessTags {
		if tag == "all" || tag == "*" {
			// If `all` or `*` is in rerunSuccessTags then we can return early
			return true
		}
		rerunSuccessTagMap[tag] = true
	}
	// Iterate over the tests that were re-ran. If any of them don't
	// allow rerun success then just exit early.
	for _, test := range testsToRerun {
		testAllowsRerunSuccess := false
		for _, tag := range test.Tags {
			if rerunSuccessTagMap[tag] {
				testAllowsRerunSuccess = true
			}
		}
		if !testAllowsRerunSuccess {
			return false
		}
	}
	return true
}
func GetBaseTestName(testName string) string {
	// If this is a non-exclusive wrapper then just return the empty string
	if nonexclusiveWrapperMatch.MatchString(testName) {
		return ""
	}

	// If the given test is a non-exclusive test with the prefix in
	// the name we'll need to pull it apart. For example:
	// non-exclusive-test-bucket-0/ext.config.files.license -> ext.config.files.license
	substrings := nonexclusivePrefixMatch.Split(testName, 2)
	return substrings[len(substrings)-1]
}

func GetRerunnableTestName(testName string) (string, bool) {
	name := GetBaseTestName(testName)
	if name == "" {
		// Test is not rerunnable if the test name matches the wrapper pattern
		return "", false
	}
	if strings.Contains(name, "/") {
		// In the case that a test is exclusive, we may
		// be adding a subtest. We don't want to do this
		return "", false
	}

	// Tests with 'warn: true' are not rerunnable
	if IsWarningOnFailure(name) {
		return "", false
	}

	// The test is not a nonexclusive wrapper, and its not a
	// subtest of an exclusive test
	return name, true
}

func IsWarningOnFailure(testName string) bool {
	for _, pattern := range WarnOnErrorTests {
		found, err := filepath.Match(pattern, testName)
		if err != nil {
			plog.Fatal(err)
			return false
		}
		if found {
			return true
		}
	}
	return false
}

func getRerunnable(testsBank map[string]*register.Test, testResults []*harness.H) map[string]*register.Test {
	// First get the names of the tests that need to rerun
	var testNamesToRerun []string
	for _, h := range testResults {
		// The current nonexclusive test wrapper would have all non-exclusive tests.
		// We would add all those tests for rerunning if none of the non-exclusive
		// subtests start due to some initial failure.
		if nonexclusiveWrapperMatch.MatchString(h.Name()) && !h.GetNonExclusiveTestStarted() {
			if h.Failed() {
				testNamesToRerun = append(testNamesToRerun, h.Subtests()...)
			}
		} else {
			name, isRerunnable := GetRerunnableTestName(h.Name())
			if h.Failed() && isRerunnable {
				testNamesToRerun = append(testNamesToRerun, name)
			}
		}
	}
	// Then convert the list of names into a list of a register.Test objects
	testsToRerun := make(map[string]*register.Test)
	for name, t := range testsBank {
		for _, rerun := range testNamesToRerun {
			if name == rerun {
				testsToRerun[name] = t
			}
		}
	}
	return testsToRerun
}

func RunTests(patterns []string, multiply int, rerun bool, rerunSuccessTags []string, pltfrm, outputDir string) error {
	return runProvidedTests(register.Tests, patterns, multiply, rerun, rerunSuccessTags, pltfrm, outputDir)
}

func RunUpgradeTests(patterns []string, rerun bool, pltfrm, outputDir string) error {
	return runProvidedTests(register.UpgradeTests, patterns, 0, rerun, nil, pltfrm, outputDir)
}

// externalTestMeta is parsed from kola.json in external tests
type externalTestMeta struct {
	Architectures             string   `json:"architectures,omitempty"             yaml:"architectures,omitempty"`
	Platforms                 string   `json:"platforms,omitempty"                 yaml:"platforms,omitempty"`
	Distros                   string   `json:"distros,omitempty"                   yaml:"distros,omitempty"`
	Tags                      string   `json:"tags,omitempty"                      yaml:"tags,omitempty"`
	RequiredTag               string   `json:"requiredTag,omitempty"               yaml:"requiredTag,omitempty"`
	AdditionalDisks           []string `json:"additionalDisks,omitempty"           yaml:"additionalDisks,omitempty"`
	InjectContainer           bool     `json:"injectContainer,omitempty"           yaml:"injectContainer,omitempty"`
	MinMemory                 int      `json:"minMemory,omitempty"                 yaml:"minMemory,omitempty"`
	MinDiskSize               int      `json:"minDisk,omitempty"                   yaml:"minDisk,omitempty"`
	AdditionalNics            int      `json:"additionalNics,omitempty"            yaml:"additionalNics,omitempty"`
	AppendKernelArgs          string   `json:"appendKernelArgs,omitempty"          yaml:"appendKernelArgs,omitempty"`
	AppendFirstbootKernelArgs string   `json:"appendFirstbootKernelArgs,omitempty" yaml:"appendFirstbootKernelArgs,omitempty"`
	Exclusive                 bool     `json:"exclusive"                           yaml:"exclusive"`
	TimeoutMin                int      `json:"timeoutMin"                          yaml:"timeoutMin"`
	Conflicts                 []string `json:"conflicts"                           yaml:"conflicts"`
	AllowConfigWarnings       bool     `json:"allowConfigWarnings"                 yaml:"allowConfigWarnings"`
	NoInstanceCreds           bool     `json:"noInstanceCreds"                     yaml:"noInstanceCreds"`
	Description               string   `json:"description"                         yaml:"description"`
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
	meta := &externalTestMeta{Exclusive: true}
	inmeta := false    // true if we saw a ## kola: prefix after which we expect YAML
	metadatayaml := "" // accumulated YAML metadata
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		// Handle the older JSON metadata
		if strings.HasPrefix(line, InstalledTestMetaPrefix) {
			if inmeta {
				return nil, fmt.Errorf("found both yaml and json test prefixes (%v %v)", InstalledTestMetaPrefixYaml, InstalledTestMetaPrefix)
			}
			buf := strings.TrimSpace(line[len(InstalledTestMetaPrefix):])
			dec := json.NewDecoder(strings.NewReader(buf))
			dec.DisallowUnknownFields()
			meta = &externalTestMeta{Exclusive: true}
			if err := dec.Decode(meta); err != nil {
				return nil, errors.Wrapf(err, "parsing %s", line)
			}
			break // We're done processing
		}

		// Look for the new YAML metadata
		if strings.HasPrefix(line, InstalledTestMetaPrefixYaml) {
			if inmeta {
				return nil, fmt.Errorf("found multiple %s", InstalledTestMetaPrefixYaml)
			}
			inmeta = true
		} else if inmeta {
			if !strings.HasPrefix(line, "## ") {
				if err := yaml.UnmarshalStrict([]byte(metadatayaml), &meta); err != nil {
					return nil, err
				}
				break
			} else {
				metadatayaml += fmt.Sprintf("%s\n", line[3:])
			}
		}
	}
	return meta, nil
}

// runExternalTest is an implementation of the "external" test framework.
// See README-kola-ext.md as well as the comments in kolet.go for reboot
// handling.
func runExternalTest(c cluster.TestCluster, mach platform.Machine, testNum int) error {
	var previousRebootState string
	var stdout []byte
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

		var cmd string
		if testNum != 0 {
			// This is a non-exclusive test
			unit := fmt.Sprintf("%s-%d.service", KoletExtTestUnit, testNum)
			// Reboot requests are disabled for non-exclusive tests
			cmd = fmt.Sprintf("sudo ./kolet run-test-unit --deny-reboots %s", shellquote.Join(unit))
		} else {
			unit := fmt.Sprintf("%s.service", KoletExtTestUnit)
			cmd = fmt.Sprintf("sudo ./kolet run-test-unit %s", shellquote.Join(unit))
		}
		stdout, err = c.SSH(mach, cmd)

		if err != nil {
			return errors.Wrapf(err, "kolet run-test-unit failed")
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

func registerExternalTest(testname, executable, dependencydir string, userdata *conf.UserData, baseMeta externalTestMeta) error {
	targetMeta, err := metadataFromTestBinary(executable)
	if err != nil {
		return errors.Wrapf(err, "Parsing metadata from %s", executable)
	}
	if targetMeta == nil {
		metaCopy := baseMeta
		targetMeta = &metaCopy
	}

	warningsAction := conf.FailWarnings
	if targetMeta.AllowConfigWarnings {
		warningsAction = conf.IgnoreWarnings
	}
	config, err := userdata.Render(warningsAction)
	if err != nil {
		return errors.Wrapf(err, "Parsing config.ign")
	}

	// Services that are exclusive will be marked by a 0 at the end of the name
	num := 0
	unitName := fmt.Sprintf("%s.service", KoletExtTestUnit)
	destDataDir := kolaExtBinDataDir
	if !targetMeta.Exclusive {
		num = extTestNum
		extTestNum += 1
		unitName = fmt.Sprintf("%s-%d.service", KoletExtTestUnit, num)
		destDataDir = fmt.Sprintf("%s-%d", kolaExtBinDataDir, num)
	}
	destDirs := make(register.DepDirMap)
	if dependencydir != "" {
		destDirs.Add(testname, dependencydir, destDataDir)
	}
	base := filepath.Base(executable)
	remotepath := fmt.Sprintf("/usr/local/bin/kola-runext-%s", base)

	// Note this isn't Type=oneshot because it's cleaner to support self-SIGTERM that way
	unit := fmt.Sprintf(`[Unit]
[Service]
RemainAfterExit=yes
EnvironmentFile=-/run/kola-runext-env
Environment=KOLA_UNIT=%s
Environment=KOLA_TEST=%s
Environment=KOLA_TEST_EXE=%s
Environment=%s=%s
ExecStart=%s
`, unitName, testname, base, kolaExtBinDataEnv, destDataDir, remotepath)
	if targetMeta.InjectContainer {
		if CosaBuild == nil {
			return fmt.Errorf("test %v uses injectContainer, but no cosa build found", testname)
		}
		ostreeContainer := CosaBuild.Meta.BuildArtifacts.Ostree
		unit += fmt.Sprintf("Environment=%s=/home/core/%s\n", kolaExtContainerDataEnv, ostreeContainer.Path)
	}
	config.AddSystemdUnit(unitName, unit, conf.NoState)

	// Architectures using 64k pages use slightly more memory, ask for more than requested
	// to make sure that we don't run out of it. Currently, only ppc64le uses 64k pages by default.
	// See similar logic in boot-mirror.go and luks.go.
	switch coreosarch.CurrentRpmArch() {
	case "ppc64le":
		if targetMeta.MinMemory <= 4096 {
			targetMeta.MinMemory = targetMeta.MinMemory * 2
		}
	}

	t := &register.Test{
		Name:          testname,
		Description:   targetMeta.Description,
		ClusterSize:   1, // Hardcoded for now
		ExternalTest:  executable,
		DependencyDir: destDirs,
		Tags:          []string{"external"},

		AdditionalDisks:           targetMeta.AdditionalDisks,
		InjectContainer:           targetMeta.InjectContainer,
		MinMemory:                 targetMeta.MinMemory,
		MinDiskSize:               targetMeta.MinDiskSize,
		AdditionalNics:            targetMeta.AdditionalNics,
		AppendKernelArgs:          targetMeta.AppendKernelArgs,
		AppendFirstbootKernelArgs: targetMeta.AppendFirstbootKernelArgs,
		NonExclusive:              !targetMeta.Exclusive,
		Conflicts:                 targetMeta.Conflicts,

		Run: func(c cluster.TestCluster) {
			mach := c.Machines()[0]
			plog.Debugf("Running kolet")

			err := runExternalTest(c, mach, num)
			if err != nil {
				out, stderr, suberr := mach.SSH(fmt.Sprintf("sudo systemctl status --lines=40 %s", shellquote.Join(unitName)))
				if len(out) > 0 {
					fmt.Printf("systemctl status %s:\n%s\n", unitName, string(out))
				} else {
					fmt.Printf("Fetching status failed: %v\n", suberr)
				}
				if mach.RuntimeConf().SSHOnTestFailure {
					plog.Errorf("dropping to shell: kolet failed: %v: %s", err, stderr)
					if err := platform.Manhole(mach); err != nil {
						plog.Errorf("failed to get terminal via ssh: %v", err)
					}
				}
				c.Fatalf(errors.Wrapf(err, "kolet failed: %s", stderr).Error())
			}
		},

		UserData: conf.Ignition(config.String()),
		Timeout:  time.Duration(targetMeta.TimeoutMin) * time.Minute,
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
	if targetMeta.NoInstanceCreds {
		t.Flags = append(t.Flags, register.NoInstanceCreds)
	}
	t.Tags = append(t.Tags, strings.Fields(targetMeta.Tags)...)
	// TODO validate tags here
	t.RequiredTag = targetMeta.RequiredTag

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

// Function that returns true if at least one test matches the given pattern
func patternMatchesTests(pattern string, tests map[string]*register.Test) (bool, error) {
	for testname := range tests {
		if match, err := filepath.Match(pattern, testname); err != nil {
			return false, err
		} else if match {
			return true, nil
		}
	}
	return false, nil
}

// registerTestDir parses one test directory and registers it as a test
func registerTestDir(dir, testprefix string, children []os.DirEntry) error {
	var dependencydir string
	var meta externalTestMeta
	userdata := conf.EmptyIgnition()
	executables := []string{}
	for _, e := range children {
		c, err := e.Info()
		if err != nil {
			return fmt.Errorf("getting info for %q: %w", e.Name(), err)
		}
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
			v, err := os.ReadFile(filepath.Join(dir, c.Name()))
			if err != nil {
				return errors.Wrapf(err, "reading %s", c.Name())
			}
			userdata = conf.Ignition(string(v))
		} else if isreg && c.Name() == "config.fcc" {
			return errors.Wrapf(err, "%s is not supported anymore; rename it to config.bu", c.Name())
		} else if isreg && (c.Name() == "config.bu") {
			v, err := os.ReadFile(filepath.Join(dir, c.Name()))
			if err != nil {
				return errors.Wrapf(err, "reading %s", c.Name())
			}
			userdata = conf.Butane(string(v))
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
			subchildren, err := os.ReadDir(subdir)
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

		err := registerExternalTest(testname, executable, dependencydir, userdata, meta)
		if err != nil {
			return err
		}
	}

	return nil
}

func RegisterExternalTestsWithPrefix(dir, prefix string) error {
	testsdir := filepath.Join(dir, "tests/kola")
	children, err := os.ReadDir(testsdir)
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

func setupExternalTest(h *harness.H, t *register.Test, tcluster cluster.TestCluster) {
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
	}
}

func collectLogsExternalTest(h *harness.H, t *register.Test, tcluster cluster.TestCluster) {
	for _, mach := range tcluster.Machines() {
		unit := fmt.Sprintf("kola-runext-%s", filepath.Base(t.ExternalTest))
		tcluster := tcluster
		// We will collect the logs in a file named according to the test name instead of the executable
		// This way if there are two executables with the same name on one machine, we avoid conflicts
		path := filepath.Join(mach.RuntimeConf().OutputDir, mach.ID(), fmt.Sprintf("%s.txt", t.Name))
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
	}
}

func createTestBuckets(tests []*register.Test) [][]*register.Test {

	// Make an array of maps. Each entry in the array represents a
	// test bucket. Each corresponding map is the test.Name -> *register.Test
	// mapping for tests to be executed
	var bucketInfo []map[string]*register.Test

	// Get a Map of test.Name -> *register.Test
	testMap := make(map[string]*register.Test)
	for _, test := range tests {
		testMap[test.Name] = test
	}

	// Update all test's conflict lists to be complete
	// FYI: it is possible that test.Conflicts contain duplicates
	// this should not affect the creation of the buckets
	for _, test := range tests {
		for _, conflict := range test.Conflicts {
			if _, found := testMap[conflict]; found {
				testMap[conflict].Conflicts = append(testMap[conflict].Conflicts, test.Name)
			} else {
				plog.Debugf("%v specified %v as a conflict but %v was not found. If you are running both tests, verify that both are marked as non-exclusive.",
					test.Name, conflict, conflict)
			}
		}
	}

	// Distribute into buckets. Start by creating a bucket with the
	// first test in it and then going from there.
	bucketInfo = append(bucketInfo, map[string]*register.Test{tests[0].Name: tests[0]})
mainloop:
	for _, test := range tests[1:] {
		for _, bucket := range bucketInfo {
			// Check if this bucket is being used by a conflicting test
			foundConflict := false
			for _, conflict := range test.Conflicts {
				if _, found := bucket[conflict]; found {
					foundConflict = true
				}
			}
			if !foundConflict {
				// No Conflict here. Assign the test and continue.
				bucket[test.Name] = test
				continue mainloop
			}
		}
		// No eligible buckets found for test. Create a new bucket.
		bucketInfo = append(bucketInfo, map[string]*register.Test{test.Name: test})
	}

	// Convert the bucketInfo array of maps into an two dimensional
	// array of register.Test objects. This is the format the caller
	// is expecting the data in.
	var buckets [][]*register.Test
	for _, bucket := range bucketInfo {
		var bucketTests []*register.Test
		for _, test := range bucket {
			bucketTests = append(bucketTests, test)
		}
		buckets = append(buckets, bucketTests)
	}

	return buckets
}

// shardTests filters tests to a particular shard - i.e. a group of tests
// whose name hashes to the same value.
func shardTests(tests map[string]*register.Test, sharding string) (map[string]*register.Test, error) {
	if sharding == "" {
		return tests, nil
	}
	if !strings.HasPrefix(sharding, "hash:") {
		return nil, fmt.Errorf("invalid sharding syntax: %s", sharding)
	}
	parts := strings.SplitN(sharding[len("hash:"):], "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid sharding syntax: %s", sharding)
	}
	mv, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid sharding syntax '%s': %w", sharding, err)
	}
	nv, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid sharding syntax '%s': %w", sharding, err)
	}
	if mv > nv || nv < 1 || mv < 1 {
		return nil, fmt.Errorf("invalid sharding in '%s'", sharding)
	}
	m := uint(mv)
	n := uint(nv)

	ret := make(map[string]*register.Test)
	for name, test := range tests {
		h := fnv.New64()
		h.Write([]byte(name))
		d := uint(h.Sum64()%uint64(n)) + 1
		if d == m {
			ret[name] = test
		}
	}
	return ret, nil
}

// Create a parent test that runs non-exclusive tests as subtests
func makeNonExclusiveTest(bucket int, tests []*register.Test, flight platform.Flight) register.Test {
	// Parse test flags and gather configs
	internetAccess := false
	var tags []string
	var nonExclusiveTestConfs []*conf.Conf
	dependencyDirs := make(register.DepDirMap)
	var subtests []string
	for _, test := range tests {
		subtests = append(subtests, test.Name)
		if test.HasFlag(register.NoSSHKeyInMetadata) || test.HasFlag(register.NoSSHKeyInUserData) {
			plog.Fatalf("Non-exclusive test %v cannot have NoSSHKeyIn* flag", test.Name)
		}
		if test.HasFlag(register.NoInstanceCreds) {
			plog.Fatalf("Non-exclusive test %v cannot have NoInstanceCreds flag", test.Name)
		}
		if test.HasFlag(register.AllowConfigWarnings) {
			plog.Fatalf("Non-exclusive test %v cannot have AllowConfigWarnings flag", test.Name)
		}
		if test.AppendKernelArgs != "" {
			plog.Fatalf("Non-exclusive test %v cannot have AppendKernelArgs", test.Name)
		}
		if !internetAccess && testRequiresInternet(test) {
			tags = append(tags, NeedsInternetTag)
			internetAccess = true
		}

		if len(test.DependencyDir) > 0 {
			for k, v := range test.DependencyDir {
				dependencyDirs[k] = v
			}
		}

		conf, err := test.UserData.Render(conf.FailWarnings)
		if err != nil {
			plog.Fatalf("Error rendering config: %v", err)
		}
		nonExclusiveTestConfs = append(nonExclusiveTestConfs, conf)
	}

	// Merge configs together
	mergedConfig, err := conf.MergeAllConfigs(nonExclusiveTestConfs)
	if err != nil {
		plog.Fatalf("Error merging configs: %v", err)
	}

	nonExclusiveWrapper := register.Test{
		Name: fmt.Sprintf("non-exclusive-test-bucket-%v", bucket),
		Run: func(tcluster cluster.TestCluster) {
			for _, t := range tests {
				t := t
				run := func(h *harness.H) {
					tcluster.H.NonExclusiveTestStarted()
					testResults.add(h)
					// tcluster has a reference to the wrapper's harness
					// We need a new TestCluster that has a reference to the
					// subtest being ran
					// This will allow timeout logic to work correctly when executing
					// functions such as TestCluster.SSH, since these functions
					// internally use harness.RunWithExecTimeoutCheck
					newTC := cluster.TestCluster{
						H:       h,
						Cluster: tcluster.Cluster,
					}
					// Install external test executable
					if t.ExternalTest != "" {
						setupExternalTest(h, t, newTC)
						// Collect the journal logs after execution is finished
						defer collectLogsExternalTest(h, t, newTC)
					}
					if IsWarningOnFailure(t.Name) {
						newTC.H.WarningOnFailure()
					}

					t.Run(newTC)
				}
				// Each non-exclusive test is run as a subtest of this wrapper test
				if t.Timeout == harness.DefaultTimeoutFlag {
					tcluster.H.RunTimeout(t.Name, run, time.Duration(1)*time.Minute)
				} else {
					tcluster.H.RunTimeout(t.Name, run, t.Timeout)
				}
			}
		},
		UserData: mergedConfig,
		Subtests: subtests,
		// This will allow runTest to copy kolet to machine
		NativeFuncs:   make(map[string]register.NativeFuncWrap),
		ClusterSize:   1,
		Tags:          tags,
		DependencyDir: dependencyDirs,
	}

	return nonExclusiveWrapper
}

// runTest is a harness for running a single test.
// outputDir is where various test logs and data will be written for
// analysis after the test run. It should already exist.
func runTest(h *harness.H, t *register.Test, pltfrm string, flight platform.Flight) {
	h.Parallel()
	h.SetSubtests(t.Subtests)

	rconf := &platform.RuntimeConfig{
		AllowFailedUnits:   testSkipBaseChecks(t),
		InternetAccess:     testRequiresInternet(t),
		NoInstanceCreds:    t.HasFlag(register.NoInstanceCreds),
		NoSSHKeyInMetadata: t.HasFlag(register.NoSSHKeyInMetadata),
		NoSSHKeyInUserData: t.HasFlag(register.NoSSHKeyInUserData),
		OutputDir:          h.OutputDir(),
		SSHOnTestFailure:   Options.SSHOnTestFailure,
		WarningsAction:     conf.FailWarnings,
		EarlyRelease:       h.Release,
	}
	if t.HasFlag(register.AllowConfigWarnings) {
		rconf.WarningsAction = conf.IgnoreWarnings
	}

	var c platform.Cluster
	c, err := flight.NewCluster(rconf)
	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		h.StopExecTimer()
		c.Destroy()
		if h.TimedOut() {
			// We'll allow tests that time out to succeed on rerun.
			markTestForRerunSuccess(t, "Test timed out.")
		}
		if testSkipBaseChecks(t) {
			plog.Debugf("Skipping base checks for %s", t.Name)
			return
		}
		handleConsoleChecks := func(logtype, id, output string) {
			warnOnly, badlines := CheckConsole([]byte(output), t)
			if SkipConsoleWarnings {
				warnOnly = true
			}
			for _, badline := range badlines {
				if warnOnly {
					plog.Warningf("Found %s on machine %s %s", badline, id, logtype)
				} else {
					h.Errorf("Found %s on machine %s %s", badline, id, logtype)
				}
			}
		}
		for id, output := range c.ConsoleOutput() {
			handleConsoleChecks("console", id, output)
		}
		for id, output := range c.JournalOutput() {
			handleConsoleChecks("journal", id, output)
		}
	}()

	if t.ClusterSize > 0 {
		var userdata *conf.UserData = t.UserData

		options := platform.MachineOptions{
			MultiPathDisk:             t.MultiPathDisk,
			AdditionalDisks:           t.AdditionalDisks,
			MinMemory:                 t.MinMemory,
			MinDiskSize:               t.MinDiskSize,
			AdditionalNics:            t.AdditionalNics,
			AppendKernelArgs:          t.AppendKernelArgs,
			AppendFirstbootKernelArgs: t.AppendFirstbootKernelArgs,
			SkipStartMachine:          true,
		}

		// Providers sometimes fail to bring up a machine within a
		// reasonable time frame. Let's try twice and then bail if
		// it doesn't work.
		err := util.Retry(2, 1*time.Second, func() error {
			var err error
			_, err = platform.NewMachines(c, userdata, t.ClusterSize, options)
			if err != nil {
				plog.Warningf("retryloop: failed to bring up machines: %v", err)
			}
			return err
		})
		if err != nil {
			// The platform failed starting machines, which usually isn't *CoreOS
			// fault. Maybe it will have better luck in the rerun.
			markTestForRerunSuccess(t, "Platform failed starting machines.")
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

	if IsWarningOnFailure(t.Name) {
		tcluster.H.WarningOnFailure()
	}

	// Note that we passed in SkipStartMachine=true in our machine
	// options. This means NewMachines() didn't block on the machines
	// being up with SSH access before returning; i.e. it skipped running
	// platform.StartMachines(). The machines should now be booting.
	// Let's start the test execution timer and then run mach.Start()
	// (wrapper for platform.StartMachine()) which sets up the journal
	// forwarding and runs machine checks, both of which require SSH
	// to be up, which implies Ignition has completed successfully.
	//
	// We do all of this so that the time it takes to run Ignition can
	// be included in our test execution timeout.
	h.StartExecTimer()
	for _, mach := range tcluster.Machines() {
		plog.Debugf("Trying to StartMachine() %v", mach.ID())
		var err error
		tcluster.RunWithExecTimeoutCheck(func() {
			err = mach.Start()
		}, fmt.Sprintf("SSH unsuccessful within allotted timeframe for %v.", mach.ID()))
		if err != nil {
			h.Fatal(errors.Wrapf(err, "mach.Start() failed"))
		}
	}

	// drop kolet binary on machines
	if t.ExternalTest != "" || t.NativeFuncs != nil {
		if err := scpKolet(tcluster.Machines()); err != nil {
			h.Fatal(err)
		}
	}

	if t.InjectContainer {
		if CosaBuild == nil {
			h.Fatalf("Test %s uses injectContainer, but no cosa build found", t.Name)
		}
		ostreeContainer := CosaBuild.Meta.BuildArtifacts.Ostree
		ostreeContainerPath := filepath.Join(CosaBuild.Dir, ostreeContainer.Path)
		if err := cluster.DropFile(tcluster.Machines(), ostreeContainerPath); err != nil {
			h.Fatal(err)
		}
	}

	if t.ExternalTest != "" {
		setupExternalTest(h, t, tcluster)
		// Collect the journal logs after execution is finished
		defer collectLogsExternalTest(h, t, tcluster)
	}

	if len(t.DependencyDir) > 0 {
		for key, dest := range t.DependencyDir {
			dir := t.DependencyDir.DirFromKey(key)
			for _, mach := range tcluster.Machines() {
				if err := platform.CopyDirToMachine(dir, mach, dest); err != nil {
					h.Fatal(errors.Wrapf(err, "copying dependencies %s to %s", dir, mach.ID()))
				}
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
	mArch := Options.CosaBuildArch
	exePath, err := os.Executable()
	if err != nil {
		return errors.Wrapf(err, "finding path of executable")
	}
	for _, d := range []string{
		".",
		filepath.Dir(exePath),
		filepath.Join(filepath.Dir(exePath), mArch),
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
// descriptions of any bad lines it finds along with a boolean
// indicating if the configuration has the bad lines marked as
// warnOnly or not (for things we don't want to error for). If t is
// specified, its flags are respected and tags possibly updated for
// rerun success.
func CheckConsole(output []byte, t *register.Test) (bool, []string) {
	var badlines []string
	warnOnly, allowRerunSuccess := true, true
	for _, check := range consoleChecks {
		if check.skipFlag != nil && t != nil && t.HasFlag(*check.skipFlag) {
			continue
		}
		match := check.match.FindSubmatch(output)
		if match != nil {
			badline := check.desc
			if len(match) > 1 {
				// include first subexpression
				badline += fmt.Sprintf(" (%s)", match[1])
			}
			badlines = append(badlines, badline)
			if !check.warnOnly {
				warnOnly = false
			}
			if !check.allowRerunSuccess {
				allowRerunSuccess = false
			}
		}
	}
	if len(badlines) > 0 && allowRerunSuccess && t != nil {
		markTestForRerunSuccess(t, "CheckConsole:")
	}
	return warnOnly, badlines
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
