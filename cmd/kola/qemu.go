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
	"github.com/coreos/mantle/platform"
)

var cmdQemu = &cobra.Command{
	Run:    runQemu,
	PreRun: preRun,
	Use:    "qemu",
	Short:  "Run and kill QEMU (requires root)",
	Long: `Run and kill QEMU

Work in progress: the code this exercises will eventually be the basis
for running automated tests on CoreOS images.

This must run as root!
`}

func init() {
	root.AddCommand(cmdQemu)
}

func runQemu(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "No args accepted\n")
		os.Exit(2)
	}

	cluster, err := platform.NewQemuCluster(kola.QEMUOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cluster failed: %v\n", err)
		os.Exit(1)
	}
	defer cluster.Destroy()

	m, err := cluster.NewMachine("#cloud-config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Machine failed: %v\n", err)
		os.Exit(1)
	}

	out, err := m.SSH("uname -a")
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH failed: %v\n", err)
		os.Exit(1)
	}
	if len(out) != 0 {
		fmt.Fprintf(os.Stdout, "SSH: %s\n", out)
	}

	err = m.Destroy()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Destroy failed: %v\n", err)
		os.Exit(1)
	}

	if len(cluster.Machines()) != 0 {
		fmt.Fprintf(os.Stderr, "Cluster not empty.\n")
		os.Exit(1)
	}

	fmt.Printf("QEMU successful!\n")
}
