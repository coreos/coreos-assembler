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
	"flag"
	"fmt"
	"os"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform"
)

var (
	cmdList = &cli.Command{

		Name:        "list-instances",
		Summary:     "List instances on GCE",
		Usage:       "-prefix=<prefix>",
		Description: " os image to Google Storage bucket and create image in GCE",
		Flags:       *flag.NewFlagSet("list", flag.ExitOnError),
		Run:         runList,
	}
	listProject string
	listZone    string
	listPrefix  string
)

func init() {
	cmdList.Flags.StringVar(&listProject, "project", "coreos-gce-testing", "found in developers console")
	cmdList.Flags.StringVar(&listZone, "zone", "us-central1-a", "gce zone")
	cmdList.Flags.StringVar(&listPrefix, "prefix", "", "prefix to filter list by")
	cli.Register(cmdList)
}

func runList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		return 2
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	vms, err := platform.GCEListVMs(client, listProject, listZone, listPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed listing vms: %v\n", err)
		return 1
	}
	for _, vm := range vms {
		fmt.Printf("%v:\n", vm.ID())
		fmt.Printf(" extIP: %v\n", vm.IP())
	}
	return 0

}
