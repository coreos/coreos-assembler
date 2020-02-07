// Copyright 2019 Red Hat, Inc.
//
// Run qemu directly as a subprocess.
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

var (
	cmdQemuExec = &cobra.Command{
		Run:    runQemuExec,
		PreRun: preRun,
		Use:    "qemuexec",
		Short:  "Directly execute qemu on a CoreOS instance",
	}

	memory  int
	usernet bool
)

func init() {
	root.AddCommand(cmdQemuExec)
	cmdQemuExec.Flags().BoolVarP(&usernet, "usernet", "U", false, "Enable usermode networking")
	cmdQemuExec.Flags().IntVarP(&memory, "memory", "m", 0, "Memory in MB")
}

func runQemuExec(cmd *cobra.Command, args []string) {
	if err := doQemuExec(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func doQemuExec(cmd *cobra.Command, args []string) error {
	var err error

	builder := platform.NewBuilder(kola.QEMUOptions.Board, "")
	defer builder.Close()

	if kola.QEMUOptions.DiskImage != "" {
		builder.AddPrimaryDisk(&platform.Disk{
			BackingFile: kola.QEMUOptions.DiskImage,
		})
	}
	if memory != 0 {
		builder.Memory = memory
	}
	if usernet {
		builder.EnableUsermodeNetworking(22)
	}
	builder.InheritConsole = true

	builder.Append(args...)

	inst, err := builder.Exec()
	if err != nil {
		return err
	}

	// Ignore errors
	_ = inst.Wait()

	return nil
}
