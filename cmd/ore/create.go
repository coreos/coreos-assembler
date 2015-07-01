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
	"io/ioutil"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
)

var (
	cmdCreate = &cobra.Command{
		Use:   "create-instances -image <gce image name> -n <number of instances>",
		Short: "Create cluster on GCE",
		Run:   runCreate,
	}
	createProject      string
	createZone         string
	createMachine      string
	createBaseName     string
	createImageName    string
	createConfig       string
	createNumInstances int
)

func init() {
	cmdCreate.Flags().StringVar(&createProject, "project", "coreos-gce-testing", "found in developers console")
	cmdCreate.Flags().StringVar(&createZone, "zone", "us-central1-a", "gce zone")
	cmdCreate.Flags().StringVar(&createMachine, "machine", "n1-standard-1", "gce machine type")
	cmdCreate.Flags().StringVar(&createBaseName, "name", "", "prefix for instances")
	cmdCreate.Flags().StringVar(&createImageName, "image", "", "GCE image name")
	cmdCreate.Flags().StringVar(&createConfig, "config", "", "path to cloud config file")
	cmdCreate.Flags().IntVar(&createNumInstances, "n", 1, "number of instances")
	root.AddCommand(cmdCreate)
}

func runCreate(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in ore create-instances: %v\n", args)
		os.Exit(2)
	}

	if createImageName == "" {
		fmt.Fprintf(os.Stderr, "Must specifcy GCE image with '-image' flag\n")
		os.Exit(2)
	}
	// if base name is unspecified use image name
	if createBaseName == "" {
		createBaseName = createImageName
	}

	var cloudConfig string
	if createConfig != "" {
		b, err := ioutil.ReadFile(createConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not read cloud config file: %v\n", err)
			os.Exit(1)
		}
		cloudConfig = string(b)
	}

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	opts := &platform.GCEOpts{
		Client:      client,
		CloudConfig: cloudConfig,
		Project:     createProject,
		Zone:        createZone,
		MachineType: createMachine,
		BaseName:    createBaseName,
		Image:       createImageName,
	}

	var vms []platform.Machine
	for i := 0; i < createNumInstances; i++ {
		vm, err := platform.GCECreateVM(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed creating vm: %v\n", err)
			os.Exit(1)
		}
		vms = append(vms, vm)
		fmt.Println("Instance created")
	}

	fmt.Printf("All instances created, add your ssh keys here: https://console.developers.google.com/project/%v/compute/metadata/sshKeys\n", createProject)
	for _, vm := range vms {
		fmt.Printf("To access %v use cmd:\n", vm.ID())
		fmt.Printf("ssh -o UserKnownHostsFile=/dev/null -o CheckHostIP=no -o StrictHostKeyChecking=no core@%v\n", vm.IP())
	}
}
