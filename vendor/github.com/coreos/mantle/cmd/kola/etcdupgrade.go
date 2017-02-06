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

	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
)

var cmdEtcdUpgrade = &cobra.Command{
	Run:   runEtcdUpgrade,
	Use:   "etcdupgrade",
	Short: "Tests etcd rolling upgrade between two given binaries.",
	Long: `
Standalone kola test that will test a rolling upgrade on etcd.

Note that this was pulled out from other automated kola tests because it
is not suited for automatic testing until supplying the paths of
pre-compiled binaries is no longer necessary. It is also not well-suited
for automated kola testing because it tests software versions that are
independant of the OS release process.

This must run as root!
`}
var (
	etcdUpgradeVersion  string
	etcdUpgradeVersion2 string
	etcdUpgradeBin      string
	etcdUpgradeBin2     string
)

func init() {
	cmdEtcdUpgrade.Flags().StringVar(&etcdUpgradeVersion, "firstEtcdVersion", "", "")
	cmdEtcdUpgrade.Flags().StringVar(&etcdUpgradeVersion2, "secondEtcdVersion", "", "")
	cmdEtcdUpgrade.Flags().StringVar(&etcdUpgradeBin, "firstBinaryPath", "", "")
	cmdEtcdUpgrade.Flags().StringVar(&etcdUpgradeBin2, "secondBinaryPath", "", "")

	root.AddCommand(cmdEtcdUpgrade)
}

// Test rolling upgrade of supplied etcd binaries
func runEtcdUpgrade(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "No args accepted\n")
		os.Exit(2)
	}

	// require flags specifiy location of etcd binaries
	if etcdUpgradeBin == "" || etcdUpgradeBin2 == "" {
		fmt.Fprintln(os.Stderr, "Must provide paths to pre-compiled etcd binaries")
		os.Exit(1)
	}

	var t = &register.Test{
		Run:         etcd.RollingUpgrade,
		ClusterSize: 3,
		Name:        "EtcdUpgrade",
		UserData: `#cloud-config

coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001`,
	}

	kola.RegisterTestOption("EtcdUpgradeVersion", etcdUpgradeVersion)
	kola.RegisterTestOption("EtcdUpgradeVersion2", etcdUpgradeVersion2)
	kola.RegisterTestOption("EtcdUpgradeBin", etcdUpgradeBin)
	kola.RegisterTestOption("EtcdUpgradeBin2", etcdUpgradeBin2)

	outputDir, err := kola.CleanOutputDir(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}

	if err := kola.RunTest(t, "gce", outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "--- FAIL: %v", err)
		os.Exit(1)
	}
}
