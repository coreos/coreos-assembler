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

	"github.com/spf13/cobra"
)

var (
	cmdDeleteBase = &cobra.Command{
		Use:   "remove-base",
		Short: "Remove base vm on ESX",
		Long:  `Remove base vm on ESX server.`,
		RunE:  runBaseDelete,

		SilenceUsage: true,
	}

	vmName string
)

func init() {
	ESX.AddCommand(cmdDeleteBase)
	cmdDeleteBase.Flags().StringVar(&vmName, "name", "", "name of base VM")
}

func runBaseDelete(cmd *cobra.Command, args []string) error {
	err := API.TerminateDevice(vmName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't delete base VM: %v\n", err)
		os.Exit(1)
	}
	return nil
}
