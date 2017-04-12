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
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	awsapi "github.com/coreos/mantle/platform/api/aws"
	gcloudapi "github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/platform/machine/aws"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/coreos/mantle/platform/machine/qemu"
	"github.com/coreos/mantle/system"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	Options     = platform.Options{}
	QEMUOptions = qemu.Options{Options: &Options}      // glue to set platform options from main
	GCEOptions  = gcloudapi.Options{Options: &Options} // glue to set platform options from main
	AWSOptions  = awsapi.Options{Options: &Options}    // glue to set platform options from main

	TestParallelism int    //glue var to set test parallelism from main
	TAPFile         string // if not "", write TAP results here
)

// NativeRunner is a closure passed to all kola test functions and used
// to run native go functions directly on kola machines. It is necessary
// glue until kola does introspection.
type NativeRunner func(funcName string, m platform.Machine) error

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
	// Avoid incurring cost of starting machine in getClusterSemver when
	// either:
	// 1) we already know 0 tests will run
	// 2) glob is an exact match which means minVersion will be ignored
	//    either way
	tests, err := filterTests(register.Tests, pattern, pltfrm, semver.Version{})
	if err != nil {
		plog.Fatal(err)
	}

	var skipGetVersion bool
	if len(tests) == 0 {
		skipGetVersion = true
	} else if len(tests) == 1 {
		for name := range tests {
			if name == pattern {
				skipGetVersion = true
			}
		}
	}

	if !skipGetVersion {
		version, err := getClusterSemver(pltfrm, outputDir)
		if err != nil {
			plog.Fatal(err)
		}

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
		fmt.Println("FAIL")
	} else {
		fmt.Println("PASS")
	}

	return err
}

// getClusterSemVer returns the CoreOS semantic version via starting a
// machine and checking
func getClusterSemver(pltfrm, outputDir string) (*semver.Version, error) {
	var err error
	var cluster platform.Cluster

	testDir := filepath.Join(outputDir, "get_cluster_semver")
	if err := os.MkdirAll(testDir, 0777); err != nil {
		return nil, err
	}

	switch pltfrm {
	case "qemu":
		cluster, err = qemu.NewCluster(&QEMUOptions, testDir)
	case "gce":
		cluster, err = gcloud.NewCluster(&GCEOptions, testDir)
	case "aws":
		cluster, err = aws.NewCluster(&AWSOptions, testDir)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}

	if err != nil {
		return nil, fmt.Errorf("creating cluster for semver check: %v", err)
	}
	defer func() {
		if err := cluster.Destroy(); err != nil {
			plog.Errorf("cluster.Destroy(): %v", err)
		}
	}()

	m, err := cluster.NewMachine("#cloud-config")
	if err != nil {
		return nil, fmt.Errorf("creating new machine for semver check: %v", err)
	}

	out, err := m.SSH("grep ^VERSION_ID= /etc/os-release")
	if err != nil {
		return nil, fmt.Errorf("parsing /etc/os-release: %v", err)
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

	var c platform.Cluster
	var err error

	testDir := h.OutputDir()
	switch pltfrm {
	case "qemu":
		c, err = qemu.NewCluster(&QEMUOptions, testDir)
	case "gce":
		c, err = gcloud.NewCluster(&GCEOptions, testDir)
	case "aws":
		c, err = aws.NewCluster(&AWSOptions, testDir)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}

	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		if err := c.Destroy(); err != nil {
			plog.Errorf("cluster.Destroy(): %v", err)
		}
	}()

	if t.ClusterSize > 0 {
		url, err := c.GetDiscoveryURL(t.ClusterSize)
		if err != nil {
			h.Fatalf("Failed to create discovery endpoint: %v", err)
		}

		cfg := strings.Replace(t.UserData, "$discovery", url, -1)
		if _, err := platform.NewMachines(c, cfg, t.ClusterSize); err != nil {
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
		nativeArch = strings.SplitN(QEMUOptions.Board, "-", 2)[0]
	}
	return nativeArch
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

// CleanOutputDir creates an empty directory, any existing data will be wiped!
func CleanOutputDir(outputDir string) (string, error) {
	outputDir = filepath.Clean(outputDir)
	if outputDir == "." {
		return "", fmt.Errorf("kola: missing output directory path")
	}
	if err := os.RemoveAll(outputDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(outputDir, 0777); err != nil {
		return "", err
	}
	return outputDir, nil
}
