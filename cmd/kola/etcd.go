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

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/cmd/kola/etcdtests"
	"github.com/coreos/mantle/platform"
)

var cmdEtcd = &cli.Command{
	Run:     runEtcd,
	Name:    "etcd",
	Summary: "Run etcd cluster under QEMU (requires root)",
	Description: `Run and kill etcd cluster

Work in progress: the code sets up an etcd cluster and then calls
integration tests to see how etcd interacts with new CoreOS images.
Currently discovery using another etcd cluster is supported for 0.4.7

This must run as root!
`}

func init() {
	cli.Register(cmdEtcd)
}

// runs and sets up environment for tests specified in etcdtests pkg
func runEtcd(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "No args accepted\n")
		return 2
	}
	for _, t := range etcdtests.Tests {
		cluster, err := platform.NewQemuCluster()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cluster failed: %v\n", err)
			return 1
		}

		url, err := cluster.GetDiscoveryURL(t.ClusterSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create discovery endpoint: %v\n", err)
			return 1
		}

		cfgs := makeConfigs(url, t.CloudConfig, t.ClusterSize)

		for i := 0; i < t.ClusterSize; i++ {
			_, err := cluster.NewMachine(cfgs[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cluster failed starting machine: %v\n", err)
				return 1
			} else {
				fmt.Fprintf(os.Stderr, "qemu instance up\n")
			}
		}

		// run test
		err = t.Run(cluster)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v failed: %v\n", t.Name, err)
			return 1
		}

		for _, m := range cluster.Machines() {
			err = m.Destroy()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Destroy failed: %v\n", err)
				return 1
			}
		}
		cluster.Destroy()
		fmt.Printf("Test %v ran successfully\n", t.Name)
	}

	fmt.Println("Etcd tests ran successfully!\n")
	return 0
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
