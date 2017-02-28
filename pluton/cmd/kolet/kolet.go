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
	"os"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola/register"

	// Register any tests that we may wish to execute in kolet.
	_ "github.com/coreos/mantle/kola"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kolet")

	root = &cobra.Command{
		Use:   "kolet",
		Short: "Native code runner for kola",
	}

	cmdRun = &cobra.Command{
		Use:   "run",
		Short: "Run a given test's native function",
	}
)

func main() {
	for testName, testObj := range register.Tests {
		if len(testObj.NativeFuncs) == 0 {
			continue
		}
		testCmd := &cobra.Command{
			Use: testName,
		}
		for nativeName := range testObj.NativeFuncs {
			nativeFunc := testObj.NativeFuncs[nativeName]
			nativeRun := func(cmd *cobra.Command, args []string) {
				if len(args) != 0 {
					cmd.Usage()
					os.Exit(2)
				}
				if err := nativeFunc(); err != nil {
					plog.Fatal(err)
				}
				// Explicitly exit successfully.
				os.Exit(0)
			}
			nativeCmd := &cobra.Command{
				Use: nativeName,
				Run: nativeRun,
			}
			testCmd.AddCommand(nativeCmd)
		}
		cmdRun.AddCommand(testCmd)
	}
	root.AddCommand(cmdRun)

	cli.Execute(root)

	// nativeRun always exits so if we are here it we probably just
	// dumped usage/help info and stopped. Must exit with non-zero
	// to prevent bugs from creating false-positives in kola.
	os.Exit(2)
}
