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

	"github.com/coreos/mantle/platform"
)

type Test struct {
	Run         func(platform.Cluster) error
	Name        string //Should be uppercase and unique
	CloudConfig string
	ClusterSize int
	Platforms   []string // whitelist of platforms to run test against -- defaults to all
}

// maps names to tests
var Tests map[string]*Test

// panic if existing name is registered
func Register(t *Test) {
	_, ok := Tests[t.Name]
	if ok {
		panic("test already registered with same name")
	}
	Tests[t.Name] = t
}

// test runner
func RunTests(args []string) int {
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Extra arguements specified. Usage: 'kola run [glob pattern]'\n")
		return 2
	}
	var pattern string
	if len(args) == 1 {
		pattern = args[0]
	} else {
		pattern = "*" // run all tests by default
	}

	var ranTests int //count successful tests
	for _, t := range Tests {
		match, err := filepath.Match(pattern, t.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		if !match {
			continue
		}

		// run all platforms if whitelist is nil
		if t.Platforms == nil {
			t.Platforms = []string{"qemu", "gce"}
		}

		for _, pltfrm := range t.Platforms {
			err := runTest(t, pltfrm)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v failed on %v: %v\n", t.Name, pltfrm, err)
				return 1
			}
			fmt.Printf("test %v ran successfully on %v\n", t.Name, pltfrm)
			ranTests++
		}
	}
	fmt.Fprintf(os.Stderr, "All %v test(s) ran successfully!\n", ranTests)
	return 0
}

// starts a cluster and runs the test
func runTest(t *Test, pltfrm string) (err error) {
	var cluster platform.Cluster
	if pltfrm == "qemu" {
		cluster, err = platform.NewQemuCluster(*QemuImage)
	} else if pltfrm == "gce" {
		cluster, err = platform.NewGCECluster(GCEOpts())
	} else {
		fmt.Fprintf(os.Stderr, "Invalid platform: %v", pltfrm)
	}

	if err != nil {
		return fmt.Errorf("Cluster failed: %v", err)
	}
	defer func() {
		if err := cluster.Destroy(); err != nil {
			fmt.Fprintf(os.Stderr, "cluster.Destroy(): %v\n", err)
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
		fmt.Fprintf(os.Stderr, "%v instance up\n", pltfrm)
	}

	// run test
	err = t.Run(cluster)
	return err
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
