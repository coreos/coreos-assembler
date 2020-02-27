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

package esx

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform/api/esx"
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "ore/esx")

	ESX = &cobra.Command{
		Use:   "esx [command]",
		Short: "esx image and vm utilities",
	}

	API     *esx.API
	options esx.Options
)

func init() {
	ESX.PersistentFlags().StringVar(&options.Server, "server", "", "ESX server")
	ESX.PersistentFlags().StringVar(&options.Profile, "profile", "", "Profile")
	cli.WrapPreRun(ESX, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running ESX Preflight check.")
	api, err := esx.New(&options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create ESX client: %v\n", err)
		os.Exit(1)
	}
	if err = api.PreflightCheck(); err != nil {
		fmt.Fprintf(os.Stderr, "could not complete ESX preflight check: %v\n", err)
		os.Exit(1)
	}

	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
