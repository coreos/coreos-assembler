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

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	QEMUOptions platform.QEMUOptions
	GCEOptions  platform.GCEOptions
	AWSOptions  platform.AWSOptions

	testOptions = make(map[string]string, 0)
)

// Registers any options that need visibility inside a Test. Panics if
// existing option is already registered. Each test has global view of
// options.
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
		err = RunTest(t, pltfrm)
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
func RunTest(t *Test, pltfrm string) error {
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

	cfgs := makeConfigs(url, t.CloudConfig, t.ClusterSize)

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
