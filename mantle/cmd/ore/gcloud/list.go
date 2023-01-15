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

package gcloud

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/platform/api/gcloud"
)

var (
	cmdList = &cobra.Command{
		Use:   "list-instances --prefix=<prefix>",
		Short: "List instances on GCE",
		Run:   runList,
	}
)

func init() {
	GCloud.AddCommand(cmdList)
}

func runList(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		os.Exit(2)
	}

	vms, err := api.ListInstances(opts.BaseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed listing vms: %v\n", err)
		os.Exit(1)
	}

	for _, vm := range vms {
		_, extIP := gcloud.InstanceIPs(vm)
		fmt.Printf("%v:\n", vm.Name)
		fmt.Printf(" extIP: %v\n", extIP)
	}
}
