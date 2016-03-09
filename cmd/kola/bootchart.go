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

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
)

var cmdBootchart = &cobra.Command{
	Run:    runBootchart,
	PreRun: preRun,
	Use:    "bootchart > bootchart.svg",
	Short:  "Boot performance graphing tool",
	Long: `
Boot a single instance and plot how the time was spent.

Note that this actually uses systemd-analyze plot rather than
systemd-bootchart since the latter requires setting a different
init process.

This must run as root!
`}

func init() {
	root.AddCommand(cmdBootchart)
}

func runBootchart(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "No args accepted\n")
		os.Exit(2)
	}

	var (
		cluster platform.Cluster
		err     error
	)
	if kolaPlatform == "qemu" {
		cluster, err = platform.NewQemuCluster(kola.QEMUOptions)
	} else if kolaPlatform == "gce" {
		cluster, err = platform.NewGCECluster(kola.GCEOptions)
	} else if kolaPlatform == "aws" {
		cluster, err = platform.NewAWSCluster(kola.AWSOptions)
	} else {
		fmt.Fprintf(os.Stderr, "Invalid platform: %v", kolaPlatform)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Cluster failed: %v\n", err)
		os.Exit(1)
	}
	defer cluster.Destroy()

	m, err := cluster.NewMachine("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Machine failed: %v\n", err)
		os.Exit(1)
	}
	defer m.Destroy()

	out, err := m.SSH("systemd-analyze plot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s", out)
}
