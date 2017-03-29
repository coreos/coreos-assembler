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

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/sdk"
)

var (
	outputDir          string
	kolaPlatform       string
	defaultTargetBoard = sdk.DefaultBoard()
	kolaDefaultImages  = map[string]string{
		"amd64-usr": sdk.BuildRoot() + "/images/amd64-usr/latest/coreos_production_image.bin",
		"arm64-usr": sdk.BuildRoot() + "/images/arm64-usr/latest/coreos_production_image.bin",
	}

	kolaDefaultBIOS = map[string]string{
		"amd64-usr": "bios-256k.bin",
		"arm64-usr": sdk.BuildRoot() + "/images/arm64-usr/latest/coreos_production_qemu_uefi_efi_code.fd",
	}
)

func init() {
	sv := root.PersistentFlags().StringVar
	bv := root.PersistentFlags().BoolVar

	// general options
	sv(&outputDir, "output-dir", "_kola_temp", "Temporary output directory for test data and logs")
	sv(&kolaPlatform, "platform", "qemu", "VM platform: qemu, gce, aws")
	root.PersistentFlags().IntVar(&kola.TestParallelism, "parallel", 1, "number of tests to run in parallel")
	sv(&kola.TAPFile, "tapfile", "", "file to write TAP results to")
	sv(&kola.Options.BaseName, "basename", "kola", "Cluster name prefix")

	// QEMU-specific options
	sv(&kola.QEMUOptions.Board, "board", defaultTargetBoard, "target board")
	sv(&kola.QEMUOptions.DiskImage, "qemu-image", "", "path to CoreOS disk image")
	sv(&kola.QEMUOptions.BIOSImage, "qemu-bios", "", "BIOS to use for QEMU vm")

	// gce-specific options
	sv(&kola.GCEOptions.Image, "gce-image", "latest", "GCE image, full api endpoints names are accepted if resource is in a different project")
	sv(&kola.GCEOptions.Project, "gce-project", "coreos-gce-testing", "GCE project name")
	sv(&kola.GCEOptions.Zone, "gce-zone", "us-central1-a", "GCE zone name")
	sv(&kola.GCEOptions.MachineType, "gce-machinetype", "n1-standard-1", "GCE machine type")
	sv(&kola.GCEOptions.DiskType, "gce-disktype", "pd-ssd", "GCE disk type")
	sv(&kola.GCEOptions.Network, "gce-network", "default", "GCE network")
	bv(&kola.GCEOptions.ServiceAuth, "gce-service-auth", false, "for non-interactive auth when running within GCE")
	sv(&kola.GCEOptions.JSONKeyFile, "gce-json-key", "", "use a service account's JSON key for authentication")

	// aws-specific options
	// CoreOS-alpha-845.0.0 on us-west-1
	sv(&kola.AWSOptions.AMI, "aws-ami", "ami-55438011", "AWS AMI ID")
	sv(&kola.AWSOptions.InstanceType, "aws-type", "t1.micro", "AWS instance type")
	sv(&kola.AWSOptions.SecurityGroup, "aws-sg", "kola", "AWS security group name")
}

// Sync up the command line options if there is dependency
func syncOptions() error {
	image, ok := kolaDefaultImages[kola.QEMUOptions.Board]
	if !ok {
		return fmt.Errorf("unsupport board %q", kola.QEMUOptions.Board)
	}

	if kola.QEMUOptions.DiskImage == "" {
		kola.QEMUOptions.DiskImage = image
	}

	if kola.QEMUOptions.BIOSImage == "" {
		kola.QEMUOptions.BIOSImage = kolaDefaultBIOS[kola.QEMUOptions.Board]
	}

	return nil
}
