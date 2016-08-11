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

	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/machine/aws"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/coreos/mantle/platform/machine/qemu"
)

var (
	cmdSpawn = &cobra.Command{
		Run:    runSpawn,
		PreRun: preRun,
		Use:    "spawn",
		Short:  "spawn a CoreOS instance",
	}

	spawnUserData string
	spawnShell    bool
	spawnRemove   bool
)

func init() {
	cmdSpawn.Flags().StringVarP(&spawnUserData, "userdata", "u", "", "userdata to pass to the instance")
	cmdSpawn.Flags().BoolVarP(&spawnShell, "shell", "s", false, "spawn a shell in the instance before exiting")
	cmdSpawn.Flags().BoolVarP(&spawnRemove, "remove", "r", true, "remove instance after shell exits")
	root.AddCommand(cmdSpawn)
}

func runSpawn(cmd *cobra.Command, args []string) {
	var userdata []byte
	var err error
	var cluster platform.Cluster

	if spawnUserData != "" {
		userdata, err = ioutil.ReadFile(spawnUserData)
		if err != nil {
			die("Reading userdata failed: %v", err)
		}
	} else {
		// ensure a key is injected
		userdata = []byte("#cloud-config")
	}

	switch kolaPlatform {
	case "qemu":
		cluster, err = qemu.NewCluster(&kola.QEMUOptions)
	case "gce":
		cluster, err = gcloud.NewCluster(&kola.GCEOptions)
	case "aws":
		cluster, err = aws.NewCluster(&kola.AWSOptions)
	default:
		err = fmt.Errorf("invalid platform %q", kolaPlatform)
	}

	if err != nil {
		die("Cluster failed: %v", err)
	}

	mach, err := cluster.NewMachine(string(userdata))
	if err != nil {
		die("Spawning instance failed: %v", err)
	}

	if spawnRemove {
		defer mach.Destroy()
	}

	if spawnShell {
		if err := platform.Manhole(mach); err != nil {
			die("Manhole failed: %v", err)
		}
	}
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
