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

	"github.com/spf13/cobra"
	"google.golang.org/api/compute/v1"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
)

var (
	cmdList = &cobra.Command{
		Use:   "list-instances -prefix=<prefix>",
		Short: "List instances on GCE",
		Run:   runList,
	}
)

func init() {
	root.AddCommand(cmdList)
}

func runList(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		os.Exit(2)
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	api, err := compute.New(client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Api Client creation failed: %v\n", err)
		os.Exit(1)
	}
	vms, err := platform.GCEListVMs(api, &opts, opts.BaseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed listing vms: %v\n", err)
		os.Exit(1)
	}
	for _, vm := range vms {
		fmt.Printf("%v:\n", vm.ID())
		fmt.Printf(" extIP: %v\n", vm.IP())
	}
}
