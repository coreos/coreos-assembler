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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/qemu"
)

var (
	cmdSpawn = &cobra.Command{
		Run:    runSpawn,
		PreRun: preRun,
		Use:    "spawn",
		Short:  "spawn a CoreOS instance",
	}

	spawnNodeCount      int
	spawnUserData       string
	spawnShell          bool
	spawnRemove         bool
	spawnVerbose        bool
	spawnMachineOptions string
)

func init() {
	cmdSpawn.Flags().IntVarP(&spawnNodeCount, "nodecount", "c", 1, "number of nodes to spawn")
	cmdSpawn.Flags().StringVarP(&spawnUserData, "userdata", "u", "", "file containing userdata to pass to the instances")
	cmdSpawn.Flags().BoolVarP(&spawnShell, "shell", "s", true, "spawn a shell in an instance before exiting")
	cmdSpawn.Flags().BoolVarP(&spawnRemove, "remove", "r", true, "remove instances after shell exits")
	cmdSpawn.Flags().BoolVarP(&spawnVerbose, "verbose", "v", false, "output information about spawned instances")
	cmdSpawn.Flags().StringVar(&spawnMachineOptions, "qemu-options", "", "experimental: path to QEMU machine options json")
	root.AddCommand(cmdSpawn)
}

func runSpawn(cmd *cobra.Command, args []string) {
	if err := doSpawn(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func doSpawn(cmd *cobra.Command, args []string) error {
	var err error

	if spawnNodeCount <= 0 {
		return fmt.Errorf("Cluster Failed: nodecount must be one or more")
	}

	var userdata *conf.UserData
	if spawnUserData != "" {
		userbytes, err := ioutil.ReadFile(spawnUserData)
		if err != nil {
			return fmt.Errorf("Reading userdata failed: %v", err)
		}
		userdata = conf.Unknown(string(userbytes))
	}

	outputDir, err = kola.SetupOutputDir(outputDir, kolaPlatform)
	if err != nil {
		return fmt.Errorf("Setup failed: %v", err)
	}

	cluster, err := kola.NewCluster(kolaPlatform, &platform.RuntimeConfig{
		OutputDir:        outputDir,
		AllowFailedUnits: true,
	})
	if err != nil {
		return fmt.Errorf("Cluster failed: %v", err)
	}

	if spawnRemove {
		defer cluster.Destroy()
	}

	var someMach platform.Machine
	for i := 0; i < spawnNodeCount; i++ {
		var mach platform.Machine
		var err error
		if kolaPlatform == "qemu" && spawnMachineOptions != "" {
			var b []byte
			b, err = ioutil.ReadFile(spawnMachineOptions)
			if err != nil {
				return fmt.Errorf("Could not read machine options: %v", err)
			}

			var machineOpts qemu.MachineOptions
			err = json.Unmarshal(b, &machineOpts)
			if err != nil {
				return fmt.Errorf("Could not unmarshal machine options: %v", err)
			}

			mach, err = cluster.(*qemu.Cluster).NewMachineWithOptions(userdata, machineOpts)
		} else {
			mach, err = cluster.NewMachine(userdata)
		}
		if err != nil {
			return fmt.Errorf("Spawning instance failed: %v", err)
		}

		if spawnVerbose {
			fmt.Printf("Machine spawned at %v\n", mach.IP())
		}

		someMach = mach
	}

	if spawnShell {
		if err := platform.Manhole(someMach); err != nil {
			return fmt.Errorf("Manhole failed: %v", err)
		}
	}
	return nil
}
