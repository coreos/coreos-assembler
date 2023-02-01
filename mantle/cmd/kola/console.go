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
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/kola"
)

var (
	cmdCheckConsole = &cobra.Command{
		Use:     "check-console [input-file...]",
		RunE:    runCheckConsole,
		PreRunE: preRun,
		Short:   "Check console output for badness.",
		Long: `
Check console output for expressions matching failure messages logged
by a Container Linux instance.

If no files are specified as arguments, stdin is checked.
`,

		SilenceUsage: true,
	}

	checkConsoleVerbose bool
)

func init() {
	cmdCheckConsole.Flags().BoolVarP(&checkConsoleVerbose, "verbose", "v", false, "output user input prompts")
	root.AddCommand(cmdCheckConsole)
}

func runCheckConsole(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// default to stdin
		args = append(args, "-")
	}

	errorcount := 0
	for _, arg := range args {
		var console []byte
		var err error
		sourceName := arg
		if arg == "-" {
			sourceName = "stdin"
			if checkConsoleVerbose {
				fmt.Printf("Reading input from %s...\n", sourceName)
			}
			console, err = io.ReadAll(os.Stdin)
		} else {
			console, err = os.ReadFile(arg)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			errorcount++
			continue
		}
		for _, badness := range kola.CheckConsole(console, nil) {
			fmt.Printf("%v: %v\n", sourceName, badness)
			errorcount++
		}
	}
	if errorcount > 0 {
		return errors.New("Errors found on console")
	}
	return nil
}
