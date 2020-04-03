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
	"fmt"
	"io/ioutil"
	"strings"

	v3 "github.com/coreos/ignition/v2/config/v3_0"
	v3types "github.com/coreos/ignition/v2/config/v3_0/types"
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
	bindro            []string
	bindrw            []string

	directIgnition       bool
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
	cmdQemuExec.Flags().BoolVar(&directIgnition, "ignition-direct", false, "Do not parse Ignition, pass directly to instance")
	cmdQemuExec.Flags().StringVarP(&ignition, "ignition", "i", "", "Path to ignition config")
	cmdQemuExec.Flags().StringArrayVar(&bindro, "bind-ro", nil, "Mount readonly via 9pfs a host directory (use --bind-ro=/path/to/host,/var/mnt/guest")
	cmdQemuExec.Flags().StringArrayVar(&bindrw, "bind-rw", nil, "Same as above, but writable")
	cmdQemuExec.Flags().BoolVarP(&forceConfigInjection, "inject-ignition", "", false, "Force injecting Ignition config using guestfs")
}

func renderFragments(config v3types.Config) (*v3types.Config, error) {
	for _, fragtype := range ignitionFragments {
		switch fragtype {
		case "autologin":
			newconf := v3.Merge(config, conf.GetAutologin())
			config = newconf
			break
		default:
			return nil, fmt.Errorf("Unknown fragment: %s", fragtype)
		}
	}
	return &config, nil
}

func parseBindOpt(s string) (string, string, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) == 1 {
		return "", "", fmt.Errorf("malformed bind option, required: SRC,DEST")
	}
	return parts[0], parts[1], nil
}

func runQemuExec(cmd *cobra.Command, args []string) error {
	var err error
	var config *v3types.Config
	if ignition != "" && !directIgnition {
		buf, err := ioutil.ReadFile(ignition)
		if err != nil {
			return err
		}
		configv, _, err := v3.Parse(buf)
		if err != nil {
			return errors.Wrapf(err, "parsing %s", ignition)
		}
		config = &configv
	}
	if len(ignitionFragments) > 0 {
		if config == nil {
			config = &v3types.Config{}
		}
		newconfig, err := renderFragments(*config)
		if err != nil {
			return errors.Wrapf(err, "rendering fragments")
		}
		config = newconfig
	}
	builder := platform.NewBuilder()
	for _, b := range bindro {
		src, dest, err := parseBindOpt(b)
		if err != nil {
			return err
		}
		builder.Mount9p(src, dest, true)
		if config == nil {
			config = &v3types.Config{}
		}
		configv := v3.Merge(*config, conf.Mount9p(dest, true))
		config = &configv
	}
	for _, b := range bindrw {
		src, dest, err := parseBindOpt(b)
		if err != nil {
			return err
		}
		builder.Mount9p(src, dest, false)
		if config == nil {
			config = &v3types.Config{}
		}
		configv := v3.Merge(*config, conf.Mount9p(dest, false))
		config = &configv
	}
	if config != nil {
		if directIgnition {
			return fmt.Errorf("Cannot use fragments/mounts with direct ignition")
		}
		if err := builder.SetConfig(*config, kola.Options.IgnitionVersion == "v2"); err != nil {
			return errors.Wrapf(err, "rendering config")
		}
	} else if directIgnition {
		builder.ConfigFile = ignition
	}
	builder.ForceConfigInjection = forceConfigInjection
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
			BackingFile:   kola.QEMUOptions.DiskImage,
			Channel:       channel,
			Size:          kola.QEMUOptions.DiskSize,
			SectorSize:    sectorSize,
			MultiPathDisk: kola.QEMUOptions.MultiPathDisk,
			NbdDisk:       kola.QEMUOptions.NbdDisk,
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
	defer inst.Destroy()

	// Ignore errors
	_ = inst.Wait()

	return nil
}
