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
)

var (
	cmdDestroy = &cli.Command{

		Name:        "destroy-instances",
		Summary:     "destroy cluster on GCE",
		Usage:       "-prefix=<prefix> ",
		Description: "Destroy GCE instances based on name prefix.",
		Flags:       *flag.NewFlagSet("upload", flag.ExitOnError),
		Run:         runDestroy,
	}
	destroyProject string
	destroyZone    string
	destroyPrefix  string
)

func init() {
	cmdDestroy.Flags.StringVar(&destroyProject, "project", "coreos-gce-testing", "found in developers console")
	cmdDestroy.Flags.StringVar(&destroyZone, "zone", "us-central1-a", "gce zone")
	cmdDestroy.Flags.StringVar(&destroyPrefix, "prefix", "", "prefix of name for instances to destroy")
	cli.Register(cmdDestroy)
}

func runDestroy(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		return 2
	}

	// avoid wiping out all instances in project or mishaps with short destroyPrefixes
	if destroyPrefix == "" || len(destroyPrefix) < 2 {
		fmt.Fprintf(os.Stderr, "Please specify a prefix of length 2 or greater with -prefix\n")
		return 1
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	vms, err := listVMs(client, destroyProject, destroyZone, destroyPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed listing vms: %v\n", err)
		return 1
	}
	var count int
	for _, vm := range vms {
		err := destroyVM(client, destroyProject, destroyZone, vm.name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed destroying vm: %v\n", err)
			return 1
		}
		count++
	}
	fmt.Printf("%v instance(s) scheduled for deletion\n", count)
	return 0
}
