// Copyright 2014 CoreOS, Inc.
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

package cli

import (
	"os"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/system/exec"
	"github.com/coreos/coreos-assembler/mantle/version"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number and exit.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("mantle/%s version %s\n",
				cmd.Root().Name(), version.Version)
		},
	}

	logDebug   bool
	logVerbose bool
	logLevel   = capnslog.NOTICE

	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "cli")
)

// Execute sets up common features that all mantle commands should share
// and then executes the command. It does not return.
func Execute(main *cobra.Command) {
	// If we were invoked via a multicall entrypoint run it instead.
	// TODO(marineam): should we figure out a way to initialize logging?
	exec.MaybeExec()

	main.AddCommand(versionCmd)

	// TODO(marineam): pflags defines the Value interface differently,
	// update capnslog accordingly...
	main.PersistentFlags().Var(&logLevel, "log-level",
		"Set global log level.")
	main.PersistentFlags().BoolVarP(&logVerbose, "verbose", "v", false,
		"Alias for --log-level=INFO")
	main.PersistentFlags().BoolVarP(&logDebug, "debug", "d", false,
		"Alias for --log-level=DEBUG")

	WrapPreRun(main, func(cmd *cobra.Command, args []string) error {
		startLogging(cmd)
		return nil
	})

	if err := main.Execute(); err != nil {
		plog.Fatal(err)
	}
	os.Exit(0)
}

func startLogging(cmd *cobra.Command) {
	switch {
	case logDebug:
		logLevel = capnslog.DEBUG
	case logVerbose:
		logLevel = capnslog.INFO
	}

	capnslog.SetFormatter(capnslog.NewStringFormatter(cmd.OutOrStderr()))
	capnslog.SetGlobalLogLevel(logLevel)

	plog.Infof("Started logging at level %s", logLevel)
}

type PreRunEFunc func(cmd *cobra.Command, args []string) error

func WrapPreRun(root *cobra.Command, f PreRunEFunc) {
	preRun, preRunE := root.PersistentPreRun, root.PersistentPreRunE
	root.PersistentPreRun, root.PersistentPreRunE = nil, nil

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := f(cmd, args); err != nil {
			return err
		}
		// Always inject startLogging to commands that are wrapping the preRun
		// due to github.com/spf13/cobra/issues/253 where parent command's
		// preRun & preRunE functions are overwritten by children
		startLogging(cmd)
		if preRun != nil {
			preRun(cmd, args)
		} else if preRunE != nil {
			return preRunE(cmd, args)
		}
		return nil
	}
}
