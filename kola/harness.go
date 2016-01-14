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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"

	// Tests imported for registration side effects.
	_ "github.com/coreos/mantle/kola/tests/coretest"
	_ "github.com/coreos/mantle/kola/tests/etcd"
	_ "github.com/coreos/mantle/kola/tests/flannel"
	_ "github.com/coreos/mantle/kola/tests/fleet"
	_ "github.com/coreos/mantle/kola/tests/ignition"
	_ "github.com/coreos/mantle/kola/tests/kubernetes"
	_ "github.com/coreos/mantle/kola/tests/metadata"
	_ "github.com/coreos/mantle/kola/tests/misc"
	_ "github.com/coreos/mantle/kola/tests/rkt"
	_ "github.com/coreos/mantle/kola/tests/systemd"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	QEMUOptions platform.QEMUOptions // glue to set platform options from main
	GCEOptions  platform.GCEOptions  // glue to set platform options from main
	AWSOptions  platform.AWSOptions  // glue to set platform options from main

	TestParallelism int //glue var to set test parallelism from main

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

func filterTests(tests map[string]*register.Test, pattern, platform string) (map[string]*register.Test, error) {
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

// RunTests is a harness for running multiple tests in parallel. Filters
// tests based on a glob pattern and by platform. Has access to all
// tests either registered in this package or by imported packages that
// register tests in their init() function.
func RunTests(pattern, pltfrm string) error {
	var passed, failed, skipped int
	var wg sync.WaitGroup

	tests, err := filterTests(register.Tests, pattern, pltfrm)
	if err != nil {
		plog.Fatal(err)
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
		err := r.result
		seconds := r.duration.Seconds()
		if err != nil && err == register.Skip {
			plog.Errorf("--- SKIP: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			skipped++
		} else if err != nil {
			plog.Errorf("--- FAIL: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			plog.Errorf("        %v", err)
			failed++
		} else {
			plog.Noticef("--- PASS: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			passed++
		}
	}

	plog.Noticef("%d passed %d failed %d skipped out of %d total", passed, failed, skipped, passed+failed+skipped)
	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	return nil
}

// RunTest is a harness for running a single test. It is used by
// RunTests but can also be used directly by binaries that aim to run a
// single test.
func RunTest(t *register.Test, pltfrm string) error {
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
		err = scpKolet(tcluster)
		if err != nil {
			return fmt.Errorf("dropping kolet binary: %v", err)
		}
	}

	// run test
	err = t.Run(tcluster)

	// give some time for the remote journal to be flushed so it can be read
	// before we run the deferred machine destruction
	if err != nil {
		time.Sleep(10 * time.Second)
	}

	return err
}

// scpKolet searches for a kolet binary and copies it to the machine.
func scpKolet(t platform.TestCluster) error {
	// TODO: determine the GOARCH for the remote machine
	mArch := "amd64"
	for _, d := range []string{
		".",
		filepath.Dir(os.Args[0]),
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
