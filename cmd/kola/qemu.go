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

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform"
)

var cmdQemu = &cli.Command{
	Run:     runQemu,
	Name:    "qemu",
	Summary: "Run and kill QEMU (requires root)",
	Description: `Run and kill QEMU

Work in progress: the code this exercises will eventually be the basis
for running automated tests on CoreOS images.

This must run as root!
`}

func init() {
	cli.Register(cmdQemu)
}

func runQemu(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "No args accepted\n")
		return 2
	}

	cluster, err := platform.NewQemuCluster()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cluster failed: %v\n", err)
		return 1
	}
	defer cluster.Destroy()

	m, err := cluster.NewMachine("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Machine failed: %v\n", err)
		return 1
	}

	out, err := m.SSH("uname -a")
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH failed: %v\n", err)
	}
	if len(out) != 0 {
		fmt.Fprintf(os.Stdout, "SSH: %s\n", out)
	}

	ssh := cluster.NewCommand("ssh",
		"-l", "core",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes",
		m.IP(),
		"uptime")

	out, err = ssh.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH command failed: %v\n", err)
		return 1
	}
	if len(out) != 0 {
		fmt.Fprintf(os.Stdout, "SSH command: %s\n", out)
	} else {
		fmt.Fprintf(os.Stderr, "SSH command produced no output.\n")
		return 1
	}

	err = m.Destroy()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Destroy failed: %v\n", err)
		return 1
	}

	if len(cluster.Machines()) != 0 {
		fmt.Fprintf(os.Stderr, "Cluster not empty.\n")
		return 1
	}

	fmt.Printf("QEMU successful!\n")
	return 0
}
