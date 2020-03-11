// Copyright 2019 Red Hat, Inc.
//
// Run qemu directly as a subprocess.
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
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
)

var (
	cmdQemuExec = &cobra.Command{
		RunE:    runQemuExec,
		PreRunE: preRun,
		Use:     "qemuexec",
		Short:   "Directly execute qemu on a CoreOS instance",

		SilenceUsage: true,
	}

	memory  int
	usernet bool

	hostname string
	ignition string
	knetargs string

	forceConfigInjection bool
)

func init() {
	root.AddCommand(cmdQemuExec)
	cmdQemuExec.Flags().StringVarP(&knetargs, "knetargs", "", "", "Arguments for Ignition networking on kernel commandline")
	cmdQemuExec.Flags().BoolVarP(&usernet, "usernet", "U", false, "Enable usermode networking")
	cmdQemuExec.Flags().StringVarP(&hostname, "hostname", "", "", "Set hostname via DHCP")
	cmdQemuExec.Flags().IntVarP(&memory, "memory", "m", 0, "Memory in MB")
	cmdQemuExec.Flags().StringVarP(&ignition, "ignition", "i", "", "Path to ignition config")
	cmdQemuExec.Flags().BoolVarP(&forceConfigInjection, "inject-ignition", "", false, "Force injecting Ignition config using guestfs")
}

func runQemuExec(cmd *cobra.Command, args []string) error {
	var err error

	builder := platform.NewBuilder(kola.QEMUOptions.Board, ignition, forceConfigInjection)
	if len(knetargs) > 0 {
		builder.IgnitionNetworkKargs = knetargs
	}
	defer builder.Close()
	builder.Firmware = kola.QEMUOptions.Firmware
	if kola.QEMUOptions.DiskImage != "" {
		channel := "virtio"
		if kola.QEMUOptions.Nvme {
			channel = "nvme"
		}
		sectorSize := 0
		if kola.QEMUOptions.Native4k {
			sectorSize = 4096
		}
		if err = builder.AddPrimaryDisk(&platform.Disk{
			BackingFile: kola.QEMUOptions.DiskImage,
			Channel:     channel,
			Size:        kola.QEMUOptions.DiskSize,
			SectorSize:  sectorSize,
		}); err != nil {
			return errors.Wrapf(err, "adding primary disk")
		}
	}
	builder.Hostname = hostname
	if memory != 0 {
		builder.Memory = memory
	}
	if usernet {
		builder.EnableUsermodeNetworking(22)
	}
	builder.InheritConsole = true

	builder.Append(args...)

	inst, err := builder.Exec()
	if err != nil {
		return err
	}

	// Ignore errors
	_ = inst.Wait()

	return nil
}
