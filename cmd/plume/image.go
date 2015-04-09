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
	cmdImage = &cli.Command{

		Name:        "list-images",
		Summary:     "List images in GCE",
		Usage:       " -prefix=<prefix>",
		Description: "List images in GCE",
		Flags:       *flag.NewFlagSet("image", flag.ExitOnError),
		Run:         runImage,
	}
	imageProject string
	imagePrefix  string
)

func init() {
	cmdImage.Flags.StringVar(&imageProject, "project", "coreos-gce-testing", "found in developers console")
	cmdImage.Flags.StringVar(&imagePrefix, "prefix", "", "prefix to filter list by")
	cli.Register(cmdImage)
}

func runImage(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		return 2
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	images, err := listImages(client, imageProject, imagePrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed listing images: %v\n", err)
		return 1
	}
	for _, image := range images {
		fmt.Printf("%v\n", image)
	}
	return 0
}
