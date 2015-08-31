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
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/options"
)

var (
	root = &cobra.Command{
		Use:   "kola [command]",
		Short: "The CoreOS Superdeep Borehole",
		// http://en.wikipedia.org/wiki/Kola_Superdeep_Borehole
	}

	cmdRun = &cobra.Command{
		Use:   "run [glob pattern]",
		Short: "Run run kola tests by category",
		Long:  "run all kola tests (default) or related groups",
		Run:   runRun,
	}

	EtcdRollingVersion     string
	EtcdRollingVersion2    string
	EtcdRollingBin         string
	EtcdRollingBin2        string
	EtcdRollingSkipVersion bool
)

func init() {
	sv := cmdRun.Flags().StringVar

	sv(&EtcdRollingVersion, "EtcdRollingVersion", "", "")
	sv(&EtcdRollingVersion2, "EtcdRollingVersion2", "", "")
	sv(&EtcdRollingBin, "EtcdRollingBin", "", "")
	sv(&EtcdRollingBin2, "EtcdRollingBin2", "", "")
	cmdRun.Flags().BoolVar(&EtcdRollingSkipVersion, "EtcdRollingSkipVersion", false, "")

	root.AddCommand(cmdRun)
}

func main() {
	cli.Execute(root)
}

func runRun(cmd *cobra.Command, args []string) {
	options.Opts = options.TestOptions{
		EtcdRollingVersion:     EtcdRollingVersion,
		EtcdRollingVersion2:    EtcdRollingVersion2,
		EtcdRollingBin:         EtcdRollingBin,
		EtcdRollingBin2:        EtcdRollingBin2,
		EtcdRollingSkipVersion: EtcdRollingSkipVersion,
	}

	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Extra arguements specified. Usage: 'kola run [glob pattern]'\n")
		os.Exit(2)
	}
	var pattern string
	if len(args) == 1 {
		pattern = args[0]
	} else {
		pattern = "*" // run all tests by default
	}

	err := kola.RunTests(pattern, kolaPlatform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
