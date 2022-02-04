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
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

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
	addDisks     []string
	usernet      bool
	cpuCountHost bool

	hostname       string
	ignition       string
	butane         string
	kargs          []string
	firstbootkargs string

	ignitionFragments []string
	bindro            []string
	bindrw            []string

	directIgnition            bool
	forceConfigInjection      bool
	propagateInitramfsFailure bool

	devshell        bool
	devshellConsole bool

	consoleFile string

	sshCommand string

	additionalNics int
)

const maxAdditionalNics = 16

func init() {
	root.AddCommand(cmdQemuExec)
	cmdQemuExec.Flags().StringVarP(&firstbootkargs, "firstbootkargs", "", "", "Additional first boot kernel arguments")
	cmdQemuExec.Flags().StringArrayVar(&kargs, "kargs", nil, "Additional kernel arguments applied")
	cmdQemuExec.Flags().BoolVarP(&usernet, "usernet", "U", false, "Enable usermode networking")
	cmdQemuExec.Flags().StringSliceVar(&ignitionFragments, "add-ignition", nil, "Append well-known Ignition fragment: [\"autologin\", \"autoresize\"]")
	cmdQemuExec.Flags().StringVarP(&hostname, "hostname", "", "", "Set hostname via DHCP")
	cmdQemuExec.Flags().IntVarP(&memory, "memory", "m", 0, "Memory in MB")
	cmdQemuExec.Flags().StringArrayVarP(&addDisks, "add-disk", "D", []string{}, "Additional disk, human readable size (repeatable)")
	cmdQemuExec.Flags().BoolVar(&cpuCountHost, "auto-cpus", false, "Automatically set number of cpus to host count")
	cmdQemuExec.Flags().BoolVar(&directIgnition, "ignition-direct", false, "Do not parse Ignition, pass directly to instance")
	cmdQemuExec.Flags().BoolVar(&devshell, "devshell", false, "Enable development shell")
	cmdQemuExec.Flags().BoolVarP(&devshellConsole, "devshell-console", "c", false, "Connect directly to serial console in devshell mode")
	cmdQemuExec.Flags().StringVarP(&ignition, "ignition", "i", "", "Path to Ignition config")
	cmdQemuExec.Flags().StringVarP(&butane, "butane", "B", "", "Path to Butane config")
	cmdQemuExec.Flags().StringArrayVar(&bindro, "bind-ro", nil, "Mount readonly via 9pfs a host directory (use --bind-ro=/path/to/host,/var/mnt/guest")
	cmdQemuExec.Flags().StringArrayVar(&bindrw, "bind-rw", nil, "Same as above, but writable")
	cmdQemuExec.Flags().BoolVarP(&forceConfigInjection, "inject-ignition", "", false, "Force injecting Ignition config using guestfs")
	cmdQemuExec.Flags().BoolVar(&propagateInitramfsFailure, "propagate-initramfs-failure", false, "Error out if the system fails in the initramfs")
	cmdQemuExec.Flags().StringVarP(&consoleFile, "console-to-file", "", "", "Filepath in which to save serial console logs")
	cmdQemuExec.Flags().IntVarP(&additionalNics, "additional-nics", "", 0, "Number of additional NICs to add")
	cmdQemuExec.Flags().StringVarP(&sshCommand, "ssh-command", "x", "", "Command to execute instead of spawning a shell")

}

func renderFragments(fragments []string, c *conf.Conf) error {
	for _, fragtype := range fragments {
		switch fragtype {
		case "autologin":
			c.AddAutoLogin()
		case "autoresize":
			c.AddAutoResize()
		default:
			return fmt.Errorf("Unknown fragment: %s", fragtype)
		}
	}
	return nil
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

	/// Qemu allows passing disk images directly, but this bypasses all of our snapshot
	/// infrastructure and it's too easy to accidentally do `cosa run foo.qcow2` instead of
	/// the more verbose (but correct) `cosa run --qemu-image foo.qcow2`.
	/// Anyone who wants persistence can add it as a disk manually.
	removeIdx := -1
	prevIsArg := false
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			prevIsArg = true
		} else {
			if !prevIsArg {
				if strings.HasSuffix(arg, ".qcow2") {
					if kola.QEMUOptions.DiskImage != "" {
						return fmt.Errorf("Multiple disk images provided")
					}
					kola.QEMUOptions.DiskImage = arg
					removeIdx = i
					continue
				}
				return fmt.Errorf("Unhandled non-option argument passed for qemu: %s", arg)
			}
			prevIsArg = false
		}
	}
	if removeIdx != -1 {
		args = append(args[:removeIdx], args[removeIdx+1:]...)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if devshellConsole {
		devshell = true

		if consoleFile != "" {
			return fmt.Errorf("Cannot use console devshell and --console-to-file")
		}

		ignitionFragments = append(ignitionFragments, "autoresize")
	}
	if devshell {
		if directIgnition {
			return fmt.Errorf("Cannot use devshell with --ignition-direct")
		}
		if kola.QEMUOptions.DiskImage == "" && kolaPlatform == "qemu" {
			return fmt.Errorf("No disk image provided")
		}
		ignitionFragments = append(ignitionFragments, "autologin")
		cpuCountHost = true
		usernet = true
		// Can't use 9p on RHEL8, need https://virtio-fs.gitlab.io/ instead in the future
		if kola.Options.CosaWorkdir != "" && !strings.HasPrefix(filepath.Base(kola.QEMUOptions.DiskImage), "rhcos") {
			// Conservatively bind readonly to avoid anything in the guest (stray tests, whatever)
			// from destroying stuff
			bindro = append(bindro, fmt.Sprintf("%s,/var/mnt/workdir", kola.Options.CosaWorkdir))
			// But provide the tempdir so it's easy to pass stuff back
			bindrw = append(bindrw, fmt.Sprintf("%s,/var/mnt/workdir-tmp", kola.Options.CosaWorkdir+"/tmp"))
		}
		if hostname == "" {
			hostname = devshellHostname
		}
	}

	if ignition != "" && butane != "" {
		return fmt.Errorf("Cannot use both --ignition and --butane")
	}
	if directIgnition && ignition == "" && butane == "" {
		return fmt.Errorf("Cannot use --ignition-direct without --ignition or --butane")
	}
	if len(ignitionFragments) > 0 && directIgnition {
		return fmt.Errorf("Cannot use --add-ignition with --ignition-direct")
	}
	if len(bindro) > 0 && directIgnition {
		return fmt.Errorf("Cannot use --bind-ro with --ignition-direct")
	}
	if len(bindrw) > 0 && directIgnition {
		return fmt.Errorf("Cannot use --bind-rw with --ignition-direct")
	}

	builder := platform.NewQemuBuilder()
	defer builder.Close()

	var config *conf.Conf
	if butane != "" {
		buf, err := ioutil.ReadFile(butane)
		if err != nil {
			return err
		}
		config, err = conf.Butane(string(buf)).Render(conf.ReportWarnings)
		if err != nil {
			return errors.Wrapf(err, "parsing %s", butane)
		}
	} else if !directIgnition && ignition != "" {
		buf, err := ioutil.ReadFile(ignition)
		if err != nil {
			return err
		}
		config, err = conf.Ignition(string(buf)).Render(conf.ReportWarnings)
		if err != nil {
			return errors.Wrapf(err, "parsing %s", ignition)
		}
	}

	ensureConfig := func() {
		if config == nil {
			config, err = conf.EmptyIgnition().Render(conf.FailWarnings)
			if err != nil {
				// could try to handle this more gratefully, but meh... this
				// really should never fail
				panic(errors.Wrapf(err, "creating empty config"))
			}
		}
	}

	if len(ignitionFragments) > 0 {
		ensureConfig()
		err := renderFragments(ignitionFragments, config)
		if err != nil {
			return errors.Wrapf(err, "rendering fragments")
		}
	}

	for _, b := range bindro {
		src, dest, err := parseBindOpt(b)
		if err != nil {
			return err
		}
		builder.Mount9p(src, dest, true)
		ensureConfig()
		config.Mount9p(dest, true)
	}
	for _, b := range bindrw {
		src, dest, err := parseBindOpt(b)
		if err != nil {
			return err
		}
		builder.Mount9p(src, dest, false)
		ensureConfig()
		config.Mount9p(dest, false)
	}
	builder.ForceConfigInjection = forceConfigInjection
	if len(firstbootkargs) > 0 {
		builder.AppendFirstbootKernelArgs = firstbootkargs
	}
	builder.AppendKernelArgs = strings.Join(kargs, " ")
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
		err = builder.AddBootDisk(&platform.Disk{
			BackingFile:   kola.QEMUOptions.DiskImage,
			Channel:       channel,
			Size:          kola.QEMUOptions.DiskSize,
			SectorSize:    sectorSize,
			MultiPathDisk: kola.QEMUOptions.MultiPathDisk,
			NbdDisk:       kola.QEMUOptions.NbdDisk,
		})
		if err != nil {
			return err
		}
	}
	if kola.QEMUIsoOptions.IsoPath != "" {
		err := builder.AddIso(kola.QEMUIsoOptions.IsoPath, "", kola.QEMUIsoOptions.AsDisk)
		if err != nil {
			return err
		}
	}
	builder.Hostname = hostname
	// for historical reasons, both --memory and --qemu-memory are supported
	if memory != 0 {
		builder.Memory = memory
	} else if kola.QEMUOptions.Memory != "" {
		parsedMem, err := strconv.ParseInt(kola.QEMUOptions.Memory, 10, 32)
		if err != nil {
			return errors.Wrapf(err, "parsing memory option")
		}
		builder.Memory = int(parsedMem)
	}
	builder.AddDisksFromSpecs(addDisks)
	if cpuCountHost {
		builder.Processors = -1
	}
	if usernet {
		h := []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
		}
		builder.EnableUsermodeNetworking(h)
	}
	if additionalNics != 0 {
		if additionalNics < 0 || additionalNics > maxAdditionalNics {
			return errors.Wrapf(nil, "additional-nics value cannot be negative or greater than %d", maxAdditionalNics)
		}
		builder.AddAdditionalNics(additionalNics)
	}
	builder.InheritConsole = true
	builder.ConsoleFile = consoleFile
	builder.Append(args...)

	if devshell && !devshellConsole {
		return runDevShellSSH(ctx, builder, config, sshCommand)
	}
	if config != nil {
		if directIgnition {
			// this shouldn't happen since we ruled out cases which trigger parsing earlier
			panic("--ignition-direct requested, but we have a parsed config")
		}
		builder.SetConfig(config)
	} else if directIgnition {
		builder.ConfigFile = ignition
	}

	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()

	if propagateInitramfsFailure {
		err := inst.WaitAll(ctx)
		if err != nil {
			return err
		}
		return nil
	}
	return inst.Wait()
}
