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
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/harness/reporters"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/torcx"
	"github.com/coreos/mantle/platform"
	awsapi "github.com/coreos/mantle/platform/api/aws"
	doapi "github.com/coreos/mantle/platform/api/do"
	esxapi "github.com/coreos/mantle/platform/api/esx"
	gcloudapi "github.com/coreos/mantle/platform/api/gcloud"
	ociapi "github.com/coreos/mantle/platform/api/oci"
	packetapi "github.com/coreos/mantle/platform/api/packet"
	"github.com/coreos/mantle/platform/machine/aws"
	"github.com/coreos/mantle/platform/machine/do"
	"github.com/coreos/mantle/platform/machine/esx"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/coreos/mantle/platform/machine/oci"
	"github.com/coreos/mantle/platform/machine/packet"
	"github.com/coreos/mantle/platform/machine/qemu"
	"github.com/coreos/mantle/system"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	Options       = platform.Options{}
	AWSOptions    = awsapi.Options{Options: &Options}    // glue to set platform options from main
	DOOptions     = doapi.Options{Options: &Options}     // glue to set platform options from main
	ESXOptions    = esxapi.Options{Options: &Options}    // glue to set platform options from main
	GCEOptions    = gcloudapi.Options{Options: &Options} // glue to set platform options from main
	OCIOptions    = ociapi.Options{Options: &Options}    // glue to set platform options from main
	PacketOptions = packetapi.Options{Options: &Options} // glue to set platform options from main
	QEMUOptions   = qemu.Options{Options: &Options}      // glue to set platform options from main

	TestParallelism   int    //glue var to set test parallelism from main
	TAPFile           string // if not "", write TAP results here
	TorcxManifestFile string // torcx manifest to expose to tests, if set
	// TorcxManifest is the unmarshalled torcx manifest file. It is available for
	// tests to access via `kola.TorcxManifest`. It will be nil if there was no
	// manifest given to kola.
	TorcxManifest *torcx.Manifest = nil

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
			desc:  "Go panic",
			match: regexp.MustCompile("panic: (.*)"),
		},
		{
			desc:  "segfault",
			match: regexp.MustCompile("SEGV"),
		},
		{
			desc:  "core dump",
			match: regexp.MustCompile("[Cc]ore dump"),
		},
	}
)

// NativeRunner is a closure passed to all kola test functions and used
// to run native go functions directly on kola machines. It is necessary
// glue until kola does introspection.
type NativeRunner func(funcName string, m platform.Machine) error

func NewCluster(pltfrm string, rconf *platform.RuntimeConfig) (cluster platform.Cluster, err error) {
	switch pltfrm {
	case "aws":
		cluster, err = aws.NewCluster(&AWSOptions, rconf)
	case "do":
		cluster, err = do.NewCluster(&DOOptions, rconf)
	case "esx":
		cluster, err = esx.NewCluster(&ESXOptions, rconf)
	case "gce":
		cluster, err = gcloud.NewCluster(&GCEOptions, rconf)
	case "oci":
		cluster, err = oci.NewCluster(&OCIOptions, rconf)
	case "packet":
		cluster, err = packet.NewCluster(&PacketOptions, rconf)
	case "qemu":
		cluster, err = qemu.NewCluster(&QEMUOptions, rconf)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}
	return
}

func filterTests(tests map[string]*register.Test, pattern, platform string, version semver.Version) (map[string]*register.Test, error) {
	r := make(map[string]*register.Test)

	for name, t := range tests {
		match, err := filepath.Match(pattern, t.Name)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}

		// Check the test's min and end versions when running more then one test
		if t.Name != pattern && versionOutsideRange(version, t.MinVersion, t.EndVersion) {
			continue
		}

		allowed := true
		for _, p := range t.Platforms {
			if p == platform {
				allowed = true
				break
			} else {
				allowed = false
			}
		}
		for _, p := range t.ExcludePlatforms {
			if p == platform {
				allowed = false
			}
		}
		if !allowed {
			continue
		}

		arch := architecture(platform)
		for _, a := range t.Architectures {
			if a == arch {
				allowed = true
				break
			} else {
				allowed = false
			}
		}
		if !allowed {
			continue
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

// RunTests is a harness for running multiple tests in parallel. Filters
// tests based on a glob pattern and by platform. Has access to all
// tests either registered in this package or by imported packages that
// register tests in their init() function.
// outputDir is where various test logs and data will be written for
// analysis after the test run. If it already exists it will be erased!
func RunTests(pattern, pltfrm, outputDir string) error {
	var versionStr string

	// Avoid incurring cost of starting machine in getClusterSemver when
	// either:
	// 1) none of the selected tests care about the version
	// 2) glob is an exact match which means minVersion will be ignored
	//    either way
	// 3) the provided torcx flag is wrong
	tests, err := filterTests(register.Tests, pattern, pltfrm, semver.Version{})
	if err != nil {
		plog.Fatal(err)
	}

	skipGetVersion := true
	for name, t := range tests {
		if name != pattern && (t.MinVersion != semver.Version{} || t.EndVersion != semver.Version{}) {
			skipGetVersion = false
			break
		}
	}

	if TorcxManifestFile != "" {
		TorcxManifest = &torcx.Manifest{}
		torcxManifestFile, err := os.Open(TorcxManifestFile)
		if err != nil {
			return errors.New("Torcx manifest path provided could not be read")
		}
		if err := json.NewDecoder(torcxManifestFile).Decode(TorcxManifest); err != nil {
			return fmt.Errorf("could not parse torcx manifest as valid json: %v", err)
		}
		torcxManifestFile.Close()
	}

	if !skipGetVersion {
		version, err := getClusterSemver(pltfrm, outputDir)
		if err != nil {
			plog.Fatal(err)
		}

		versionStr = version.String()

		// one more filter pass now that we know real version
		tests, err = filterTests(tests, pattern, pltfrm, *version)
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
			runTest(h, test, pltfrm)
		}
		htests.Add(test.Name, run)
	}

	suite := harness.NewSuite(opts, htests)
	err = suite.Run()

	if TAPFile != "" {
		src := filepath.Join(outputDir, "test.tap")
		if err2 := system.CopyRegularFile(src, TAPFile); err == nil && err2 != nil {
			err = err2
		}
	}

	if err != nil {
		fmt.Printf("FAIL, output in %v\n", outputDir)
	} else {
		fmt.Printf("PASS, output in %v\n", outputDir)
	}

	return err
}

// getClusterSemVer returns the CoreOS semantic version via starting a
// machine and checking
func getClusterSemver(pltfrm, outputDir string) (*semver.Version, error) {
	var err error

	testDir := filepath.Join(outputDir, "get_cluster_semver")
	if err := os.MkdirAll(testDir, 0777); err != nil {
		return nil, err
	}

	cluster, err := NewCluster(pltfrm, &platform.RuntimeConfig{
		OutputDir: testDir,
	})
	if err != nil {
		return nil, fmt.Errorf("creating cluster for semver check: %v", err)
	}
	defer func() {
		if err := cluster.Destroy(); err != nil {
			plog.Errorf("cluster.Destroy(): %v", err)
		}
	}()

	m, err := cluster.NewMachine(nil)
	if err != nil {
		return nil, fmt.Errorf("creating new machine for semver check: %v", err)
	}

	out, stderr, err := m.SSH("grep ^VERSION_ID= /etc/os-release")
	if err != nil {
		return nil, fmt.Errorf("parsing /etc/os-release: %v: %s", err, stderr)
	}

	version, err := semver.NewVersion(strings.Split(string(out), "=")[1])
	if err != nil {
		return nil, fmt.Errorf("parsing os-release semver: %v", err)
	}

	return version, nil
}

// runTest is a harness for running a single test.
// outputDir is where various test logs and data will be written for
// analysis after the test run. It should already exist.
func runTest(h *harness.H, t *register.Test, pltfrm string) {
	h.Parallel()

	// don't go too fast, in case we're talking to a rate limiting api like AWS EC2.
	// FIXME(marineam): API requests must do their own
	// backoff due to rate limiting, this is unreliable.
	max := int64(2 * time.Second)
	splay := time.Duration(rand.Int63n(max))
	time.Sleep(splay)

	rconf := &platform.RuntimeConfig{
		OutputDir:          h.OutputDir(),
		NoSSHKeyInUserData: t.HasFlag(register.NoSSHKeyInUserData),
		NoSSHKeyInMetadata: t.HasFlag(register.NoSSHKeyInMetadata),
		NoEnableSelinux:    t.HasFlag(register.NoEnableSelinux),
	}
	c, err := NewCluster(pltfrm, rconf)
	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		if err := c.Destroy(); err != nil {
			plog.Errorf("cluster.Destroy(): %v", err)
		}
		for id, output := range c.ConsoleOutput() {
			for _, badness := range CheckConsole([]byte(output), t) {
				h.Errorf("Found %s on machine %s console", badness, id)
			}
		}
	}()

	if t.ClusterSize > 0 {
		userdata := t.UserData
		if userdata != nil && userdata.Contains("$discovery") {
			url, err := c.GetDiscoveryURL(t.ClusterSize)
			if err != nil {
				h.Fatalf("Failed to create discovery endpoint: %v", err)
			}
			userdata = userdata.Subst("$discovery", url)
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
	}

	// drop kolet binary on machines
	if t.NativeFuncs != nil {
		scpKolet(tcluster, architecture(pltfrm))
	}

	defer func() {
		// give some time for the remote journal to be flushed so it can be read
		// before we run the deferred machine destruction
		time.Sleep(2 * time.Second)
	}()

	// run test
	t.Run(tcluster)
}

// architecture returns the machine architecture of the given platform.
func architecture(pltfrm string) string {
	nativeArch := "amd64"
	if pltfrm == "qemu" && QEMUOptions.Board != "" {
		nativeArch = boardToArch(QEMUOptions.Board)
	}
	if pltfrm == "packet" && PacketOptions.Board != "" {
		nativeArch = boardToArch(PacketOptions.Board)
	}
	return nativeArch
}

// returns the arch part of an sdk board name
func boardToArch(board string) string {
	return strings.SplitN(board, "-", 2)[0]
}

// scpKolet searches for a kolet binary and copies it to the machine.
func scpKolet(c cluster.TestCluster, mArch string) {
	for _, d := range []string{
		".",
		filepath.Dir(os.Args[0]),
		filepath.Join(filepath.Dir(os.Args[0]), mArch),
		filepath.Join("/usr/lib/kola", mArch),
	} {
		kolet := filepath.Join(d, "kolet")
		if _, err := os.Stat(kolet); err == nil {
			if err := c.DropFile(kolet); err != nil {
				c.Fatalf("dropping kolet binary: %v", err)
			}
			return
		}
	}
	c.Fatalf("Unable to locate kolet binary for %s", mArch)
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
