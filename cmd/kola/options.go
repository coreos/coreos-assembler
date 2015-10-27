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
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/sdk"
)

var (
	kolaPlatform string
)

func init() {
	sv := root.PersistentFlags().StringVar
	bv := root.PersistentFlags().BoolVar

	// general options
	sv(&kolaPlatform, "platform", "qemu", "VM platform: qemu, gce, aws")
	root.PersistentFlags().IntVar(&kola.TestParallelism, "parallel", 1, "number of tests to run in parallel")

	sv(&kola.QEMUOptions.DiskImage, "qemu-image", sdk.BuildRoot()+"/images/amd64-usr/latest/coreos_production_image.bin", "path to CoreOS disk image")

	// gce specific options
	sv(&kola.GCEOptions.Image, "gce-image", "latest", "GCE image")
	sv(&kola.GCEOptions.Project, "gce-project", "coreos-gce-testing", "GCE project name")
	sv(&kola.GCEOptions.Zone, "gce-zone", "us-central1-a", "GCE zone name")
	sv(&kola.GCEOptions.MachineType, "gce-machinetype", "n1-standard-1", "GCE machine type")
	sv(&kola.GCEOptions.DiskType, "gce-disktype", "pd-ssd", "GCE disk type")
	sv(&kola.GCEOptions.BaseName, "gce-basename", "kola", "GCE instance name prefix")
	sv(&kola.GCEOptions.Network, "gce-network", "default", "GCE network")
	bv(&kola.GCEOptions.ServiceAuth, "gce-service-auth", false, "for non-interactive auth when running within GCE")

	// aws specific options
	// CoreOS-alpha-845.0.0 on us-west-1
	sv(&kola.AWSOptions.AMI, "aws-ami", "ami-55438011", "AWS AMI ID")
	sv(&kola.AWSOptions.KeyName, "aws-key", "", "AWS SSH key name")
	sv(&kola.AWSOptions.InstanceType, "aws-type", "t1.micro", "AWS instance type")
	sv(&kola.AWSOptions.SecurityGroup, "aws-sg", "kola", "AWS security group name")
}
