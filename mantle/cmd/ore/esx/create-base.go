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
	cmdCreateBase = &cobra.Command{
		Use:   "create-base",
		Short: "Create base vm on ESX",
		Long: `Upload an OVF and create a base VM.

After a successful run, the final line of output will be the name of the VM created.
`,
		RunE: runBaseCreate,

		SilenceUsage: true,
	}

	ovaPath    string
	baseVMName string
)

func init() {
	ESX.AddCommand(cmdCreateBase)
	cmdCreateBase.Flags().StringVar(&ovaPath, "file", "", "path to VMware OVA image")
	cmdCreateBase.Flags().StringVar(&baseVMName, "name", "", "name of base VM")
}

func runBaseCreate(cmd *cobra.Command, args []string) error {
	if ovaPath == "" {
		fmt.Fprintf(os.Stderr, "--file is required\n")
		os.Exit(1)
	}
	if baseVMName == "" {
		fmt.Fprintf(os.Stderr, "--name is required\n")
		os.Exit(1)
	}

	err := API.CreateBaseDevice(baseVMName, ovaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create base VM: %v\n", err)
		os.Exit(1)
	}
	return nil
}
