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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	ignconverter "github.com/coreos/ign-converter"
	v3 "github.com/coreos/ignition/v2/config/v3_0"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

var (
	cmdQemuExec = &cobra.Command{
		RunE:    runQemuExec,
		PreRunE: preRun,
		Use:     "qemuexec",
		Short:   "Directly execute qemu on a CoreOS instance",

		SilenceUsage: true,
	}

	memory       int
	usernet      bool
	cpuCountHost bool

	hostname string
	ignition string
	kargs    string
	knetargs string

	ignitionFragments []string

	forceConfigInjection bool
)

func init() {
	root.AddCommand(cmdQemuExec)
	cmdQemuExec.Flags().StringVarP(&knetargs, "knetargs", "", "", "Arguments for Ignition networking on kernel commandline")
	cmdQemuExec.Flags().StringVarP(&kargs, "kargs", "", "", "Additional kernel arguments applied")
	cmdQemuExec.Flags().BoolVarP(&usernet, "usernet", "U", false, "Enable usermode networking")
	cmdQemuExec.Flags().StringSliceVar(&ignitionFragments, "add-ignition", nil, "Append well-known Ignition fragment: [\"autologin\"]")
	cmdQemuExec.Flags().StringVarP(&hostname, "hostname", "", "", "Set hostname via DHCP")
	cmdQemuExec.Flags().IntVarP(&memory, "memory", "m", 0, "Memory in MB")
	cmdQemuExec.Flags().BoolVar(&cpuCountHost, "auto-cpus", false, "Automatically set number of cpus to host count")
	cmdQemuExec.Flags().StringVarP(&ignition, "ignition", "i", "", "Path to ignition config")
	cmdQemuExec.Flags().BoolVarP(&forceConfigInjection, "inject-ignition", "", false, "Force injecting Ignition config using guestfs")
}

func renderFragments() (string, error) {
	buf, err := ioutil.ReadFile(ignition)
	if err != nil {
		return "", err
	}
	config, _, err := v3.Parse(buf)
	for _, fragtype := range ignitionFragments {
		switch fragtype {
		case "autologin":
			newconf := v3.Merge(config, conf.GetAutologin())
			config = newconf
			break
		default:
			return "", fmt.Errorf("Unknown fragment: %s", fragtype)
		}
	}

	newbuf, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	tmpf, err := ioutil.TempFile("", "qemuexec-ign")
	if err != nil {
		return "", err
	}
	if _, err := tmpf.Write(newbuf); err != nil {
		return "", err
	}

	return tmpf.Name(), nil
}

func runQemuExec(cmd *cobra.Command, args []string) error {
	var err error
	cleanupIgnition := false
	if len(ignitionFragments) > 0 {
		newconfig, err := renderFragments()
		if err != nil {
			return errors.Wrapf(err, "rendering fragments")
		}
		ignition = newconfig
		cleanupIgnition = true
	}
	if kola.Options.IgnitionVersion == "v2" {
		buf, err := ioutil.ReadFile(ignition)
		if err != nil {
			return err
		}
		config, _, err := v3.Parse(buf)
		if err != nil {
			return errors.Wrapf(err, "parsing %s", ignition)
		}
		ignc2, err := ignconverter.Translate3to2(config)
		if err != nil {
			return err
		}
		ignc2buf, err := json.Marshal(ignc2)
		if err != nil {
			return err
		}
		tmpf, err := ioutil.TempFile("", "qemuexec-ign-conv")
		if err != nil {
			return err
		}
		if _, err := tmpf.Write(ignc2buf); err != nil {
			return err
		}
		if cleanupIgnition {
			os.Remove(ignition)
		}
		cleanupIgnition = true
		ignition = tmpf.Name()
	}
	defer func() {
		if cleanupIgnition {
			os.Remove(ignition)
		}
	}()

	builder := platform.NewBuilder(ignition, forceConfigInjection)
	if len(knetargs) > 0 {
		builder.IgnitionNetworkKargs = knetargs
	}
	builder.AppendKernelArguments = kargs
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
	if cpuCountHost {
		builder.Processors = -1
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
