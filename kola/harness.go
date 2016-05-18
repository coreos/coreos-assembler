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
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/skip"
	"github.com/coreos/mantle/platform"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/go-semver/semver"
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"

	// Tests imported for registration side effects.
	_ "github.com/coreos/mantle/kola/tests/coretest"
	_ "github.com/coreos/mantle/kola/tests/docker"
	_ "github.com/coreos/mantle/kola/tests/etcd"
	_ "github.com/coreos/mantle/kola/tests/flannel"
	_ "github.com/coreos/mantle/kola/tests/fleet"
	_ "github.com/coreos/mantle/kola/tests/ignition/v1"
	_ "github.com/coreos/mantle/kola/tests/ignition/v2"
	_ "github.com/coreos/mantle/kola/tests/kubernetes"
	_ "github.com/coreos/mantle/kola/tests/metadata"
	_ "github.com/coreos/mantle/kola/tests/misc"
	_ "github.com/coreos/mantle/kola/tests/rkt"
	_ "github.com/coreos/mantle/kola/tests/systemd"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	Options     = platform.Options{}
	QEMUOptions = platform.QEMUOptions{Options: &Options} // glue to set platform options from main
	GCEOptions  = platform.GCEOptions{Options: &Options}  // glue to set platform options from main
	AWSOptions  = platform.AWSOptions{Options: &Options}  // glue to set platform options from main

	TestParallelism int    //glue var to set test parallelism from main
	TAPFile         string // if not "", write TAP results here

	testOptions = make(map[string]string, 0)
)

// RegisterTestOption registers any options that need visibility inside
// a Test. Panics if existing option is already registered. Each test
// has global view of options.
func RegisterTestOption(name, option string) {
	_, ok := testOptions[name]
	if ok {
		panic("test option already registered with same name")
	}
	testOptions[name] = option
}

// NativeRunner is a closure passed to all kola test functions and used
// to run native go functions directly on kola machines. It is necessary
// glue until kola does introspection.
type NativeRunner func(funcName string, m platform.Machine) error

type result struct {
	test     *register.Test
	result   error
	duration time.Duration
}

type resultSlice []*result

// emit tap results.
// https://testanything.org/tap-version-13-specification.html
func (res resultSlice) ToTAP(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "TAP version 13\n1..%d\n", len(res)); err != nil {
		return err
	}

	var werr error

	for i, r := range res {
		t := r.test

		switch err := r.result.(type) {
		case nil:
			_, werr = fmt.Fprintf(w, "ok %d - %s\n", i+1, t.Name)
		case skip.Skip:
			_, werr = fmt.Fprintf(w, "ok %d - %s # SKIP: %s\n", i+1, t.Name, err)
		default:
			_, werr = fmt.Fprintf(w, "not ok %d - %s # %s\n", i+1, t.Name, err)
		}

		if werr != nil {
			return werr
		}
	}

	return nil
}

func testRunner(platform string, done <-chan struct{}, tests chan *register.Test, results chan *result) {
	for test := range tests {
		plog.Noticef("=== RUN %s on %s", test.Name, platform)
		start := time.Now()
		err := RunTest(test, platform)
		duration := time.Since(start)

		select {
		case results <- &result{test, err, duration}:
		case <-done:
			return
		}
	}
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

		// Skip the test if Manual is set and the name doesn't fully match.
		if t.Manual && t.Name != pattern {
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
func RunTests(pattern, pltfrm string) error {
	var passed, failed, skipped int
	var wg sync.WaitGroup
	var results resultSlice

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
		version, err := getClusterSemver(pltfrm)
		if err != nil {
			plog.Fatal(err)
		}

		// one more filter pass now that we know real version
		tests, err = filterTests(tests, pattern, pltfrm, *version)
		if err != nil {
			plog.Fatal(err)
		}
	}

	done := make(chan struct{})
	defer close(done)
	testc := make(chan *register.Test)
	resc := make(chan *result)

	wg.Add(TestParallelism)

	for i := 0; i < TestParallelism; i++ {
		go func() {
			testRunner(pltfrm, done, testc, resc)
			wg.Done()
		}()
	}
	go func() {
		wg.Wait()
		close(resc)
	}()

	// feed pipeline
	go func() {
		for _, t := range tests {
			testc <- t

			// don't go too fast, in case we're talking to a rate limiting api like AWS EC2.
			time.Sleep(2 * time.Second)
		}
		close(testc)
	}()

	for r := range resc {
		t := r.test
		seconds := r.duration.Seconds()
		switch err := r.result.(type) {
		case nil:
			plog.Noticef("--- PASS: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			passed++
		case skip.Skip:
			plog.Errorf("--- SKIP: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			plog.Errorf("        %v", err)
			skipped++
		default:
			plog.Errorf("--- FAIL: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			plog.Errorf("        %v", err)
			failed++
		}

		results = append(results, r)
	}

	if TAPFile != "" {
		f, err := os.Create(TAPFile)
		if err != nil {
			plog.Errorf("failed to create TAP file: %v", err)
		} else {
			if err := results.ToTAP(f); err != nil {
				plog.Errorf("failed write TAP results: %v", err)
			}
			f.Close()
		}
	}

	plog.Noticef("%d passed %d failed %d skipped out of %d total", passed, failed, skipped, passed+failed+skipped)
	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	return nil
}

// getClusterSemVer returns the CoreOS semantic version via starting a
// machine and checking
func getClusterSemver(pltfrm string) (*semver.Version, error) {
	var err error
	var cluster platform.Cluster

	switch pltfrm {
	case "qemu":
		cluster, err = platform.NewQemuCluster(QEMUOptions)
	case "gce":
		cluster, err = platform.NewGCECluster(GCEOptions)
	case "aws":
		cluster, err = platform.NewAWSCluster(AWSOptions)
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

// RunTest is a harness for running a single test. It is used by
// RunTests but can also be used directly by binaries that aim to run a
// single test. Using RunTest directly means that TestCluster flags used
// to filter out tests such as 'Platforms', 'Manual', or 'MinVersion'
// are not respected.
func RunTest(t *register.Test, pltfrm string) (err error) {
	var cluster platform.Cluster

	switch pltfrm {
	case "qemu":
		cluster, err = platform.NewQemuCluster(QEMUOptions)
	case "gce":
		cluster, err = platform.NewGCECluster(GCEOptions)
	case "aws":
		cluster, err = platform.NewAWSCluster(AWSOptions)
	default:
		err = fmt.Errorf("invalid platform %q", pltfrm)
	}

	if err != nil {
		return fmt.Errorf("Cluster failed: %v", err)
	}
	defer func() {
		if err := cluster.Destroy(); err != nil {
			plog.Errorf("cluster.Destroy(): %v", err)
		}
	}()

	url, err := cluster.GetDiscoveryURL(t.ClusterSize)
	if err != nil {
		return fmt.Errorf("Failed to create discovery endpoint: %v", err)
	}

	cfgs := makeConfigs(url, t.UserData, t.ClusterSize)

	if t.ClusterSize > 0 {
		_, err := platform.NewMachines(cluster, cfgs)
		if err != nil {
			return fmt.Errorf("Cluster failed starting machines: %v", err)
		}
	}

	// pass along all registered native functions
	var names []string
	for k := range t.NativeFuncs {
		names = append(names, k)
	}

	// prevent unsafe access if tests ever become parallel and access
	tempTestOptions := make(map[string]string, 0)
	for k, v := range testOptions {
		tempTestOptions[k] = v
	}

	// Cluster -> TestCluster
	tcluster := platform.TestCluster{
		Name:        t.Name,
		NativeFuncs: names,
		Options:     tempTestOptions,
		Cluster:     cluster,
	}

	// drop kolet binary on machines
	if t.NativeFuncs != nil {
		nativeArch := "amd64"
		if pltfrm == "qemu" && QEMUOptions.Board != "" {
			nativeArch = strings.SplitN(QEMUOptions.Board, "-", 2)[0]
		}

		err = scpKolet(tcluster, nativeArch)
		if err != nil {
			return fmt.Errorf("dropping kolet binary: %v", err)
		}
	}

	defer func() {
		r := recover()
		switch r := r.(type) {
		case nil:
			// no-op
		case error:
			err = r
		default:
			err = fmt.Errorf("test panicked: %v", r)
		}

		// give some time for the remote journal to be flushed so it can be read
		// before we run the deferred machine destruction
		if err != nil {
			time.Sleep(2 * time.Second)
		}
	}()

	// run test
	return t.Run(tcluster)
}

// scpKolet searches for a kolet binary and copies it to the machine.
func scpKolet(t platform.TestCluster, mArch string) error {
	for _, d := range []string{
		".",
		filepath.Dir(os.Args[0]),
		filepath.Join(filepath.Dir(os.Args[0]), mArch),
		filepath.Join("/usr/lib/kola", mArch),
	} {
		kolet := filepath.Join(d, "kolet")
		if _, err := os.Stat(kolet); err == nil {
			return t.DropFile(kolet)
		}
	}
	return fmt.Errorf("Unable to locate kolet binary for %s", mArch)
}

// replaces $discovery with discover url in etcd cloud config and
// replaces $name with a unique name
func makeConfigs(url, cfg string, csize int) []string {
	cfg = strings.Replace(cfg, "$discovery", url, -1)

	var cfgs []string
	for i := 0; i < csize; i++ {
		cfgs = append(cfgs, strings.Replace(cfg, "$name", "instance"+strconv.Itoa(i), -1))
	}
	return cfgs
}
