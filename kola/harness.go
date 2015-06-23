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
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/platform"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

// NativeRunner is a closure passed to all kola test functions and used
// to run native go functions directly on kola machines. It is necessary
// glue until kola does introspection.
type NativeRunner func(funcName string, m platform.Machine) error

type Test struct {
	Name        string // should be uppercase and unique
	Run         func(platform.TestCluster) error
	NativeFuncs map[string]func() error
	CloudConfig string
	ClusterSize int
	Platforms   []string // whitelist of platforms to run test against -- defaults to all
}

// maps names to tests
var Tests = map[string]*Test{}

// panic if existing name is registered
func Register(t *Test) {
	_, ok := Tests[t.Name]
	if ok {
		panic("test already registered with same name")
	}
	Tests[t.Name] = t
}

// test runner and kola entry point
func RunTests(pattern, pltfrm string) error {
	var passed, failed int

	for _, t := range Tests {
		match, err := filepath.Match(pattern, t.Name)
		if err != nil {
			plog.Error(err)
		}
		if !match {
			continue
		}

		allowed := true
		for _, p := range t.Platforms {
			if p == pltfrm {
				allowed = true
				break
			} else {
				allowed = false
			}
		}
		if !allowed {
			continue
		}

		start := time.Now()
		plog.Noticef("=== RUN %s on %s", t.Name, pltfrm)
		err = runTest(t, pltfrm)
		seconds := time.Since(start).Seconds()
		if err != nil {
			plog.Errorf("--- FAIL: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			plog.Errorf("        %v", err)
			failed++
		} else {
			plog.Noticef("--- PASS: %s on %s (%.3fs)", t.Name, pltfrm, seconds)
			passed++
		}
	}

	plog.Noticef("%d passed %d failed out of %d total", passed, failed, passed+failed)
	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	return nil
}

// create a cluster and run test
func runTest(t *Test, pltfrm string) error {
	var err error
	var cluster platform.Cluster

	if pltfrm == "qemu" {
		cluster, err = platform.NewQemuCluster(*QemuImage)
	} else if pltfrm == "gce" {
		cluster, err = platform.NewGCECluster(GCEOpts())
	} else {
		plog.Errorf("Invalid platform: %v", pltfrm)
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

	cfgs := makeConfigs(url, t.CloudConfig, t.ClusterSize)

	for i := 0; i < t.ClusterSize; i++ {
		_, err := cluster.NewMachine(cfgs[i])
		if err != nil {
			return fmt.Errorf("Cluster failed starting machine: %v", err)
		}
		plog.Infof("%v instance up", pltfrm)
	}
	// pass along all registered native functions
	var names []string
	for k := range t.NativeFuncs {
		names = append(names, k)
	}
	// Cluster -> TestCluster
	tcluster := platform.TestCluster{t.Name, names, cluster}

	// drop kolet binary on machines
	if t.NativeFuncs != nil {
		err = scpKolet(tcluster)
		if err != nil {
			return fmt.Errorf("dropping kolet binary: %v", err)
		}
	}

	// run test
	err = t.Run(tcluster)
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
