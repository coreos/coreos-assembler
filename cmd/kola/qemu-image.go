// Copyright 2017 CoreOS, Inc.
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

	"github.com/coreos/mantle/platform/machine/qemu"
)

var cmdMkImage = &cobra.Command{
	Use:    "mkimage <input> <output>",
	Run:    runMkImage,
	PreRun: preRun,
	Short:  "Specialize image for running kola qemu tests (requires root)",
	Long: `
Copy a Container Linux image.bin and add optional (but helpful) bits for
running kola tests.

This must run as root!
`}

func init() {
	root.AddCommand(cmdMkImage)
}

func runMkImage(cmd *cobra.Command, args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Input and output image required\n")
		os.Exit(2)
	}
	if len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Too many arguments\n")
		os.Exit(2)
	}

	err := qemu.MakeDiskTemplate(args[0], args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
