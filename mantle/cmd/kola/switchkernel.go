// Copyright 2020 Red Hat, Inc.
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
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	cmdSwitchKernel = &cobra.Command{
		RunE:    runSwitchKernel,
		PreRunE: preRun,
		Use:     "switch-kernel",
		Short:   "Test on switching between Default and RT Kernel",

		SilenceUsage: true,
	}

	rtKernelRpmDir string
)

var (
	homeDir            = `/var/home/core`
	switchKernelScript = `#!/usr/bin/env bash
	# This script is a shameless translation of: https://github.com/openshift/machine-config-operator/blob/f363c7be6d2d506d900e196fa2e2d05ca08b93b6/pkg/daemon/update.go#L651
	# Usage:
	# switch-kernel oldkernel newkernel rt-kernel-repo
	# {oldkernel, newkernel}: either of {default, rt-kernel}
	# rt-kernel-repo (optional): repository of kernel-rt packages
	set -xeuo pipefail
	
	FROM_KERNEL="${1:?old kernel should be either of \{default, rt-kernel\}}"
	TO_KERNEL="${2:?new kernel should be either of \{default, rt-kernel\}}"
	
	DEFAULT_KERNEL_PKG="kernel kernel-core kernel-modules kernel-modules-extra"
	RT_KERNEL_PKG="kernel-rt-core kernel-rt-modules kernel-rt-modules-extra"
	
	if [[ $FROM_KERNEL == "default" && $TO_KERNEL == "rt-kernel" ]]; then
		# Switch from default to RT Kernel
		# https://github.com/openshift/machine-config-operator/blob/master/pkg/daemon/update.go#L711
		RT_KERNEL_REPO=$3
		if [[ -z $(ls ${RT_KERNEL_REPO}) ]]; then
			echo "No kernel-rt package available in the repo: ${RT_KERNEL_REPO}"
			exit 1
		fi
	
		ARGS="override remove ${DEFAULT_KERNEL_PKG}"
		for RPM in $(ls ${RT_KERNEL_REPO})
		do
			ARGS+=" --install ${RT_KERNEL_REPO}/${RPM}"
		done
		rpm-ostree ${ARGS}
	elif [[ $FROM_KERNEL == "rt-kernel" && $TO_KERNEL == "default" ]]; then
		# Switch from RT Kernel to default
		# https://github.com/openshift/machine-config-operator/blob/e246be62e7839a086bc4494203472349c406dcae/pkg/daemon/update.go#L667
		ARGS="override reset ${DEFAULT_KERNEL_PKG}"
		for PKG in $RT_KERNEL_PKG
		do
			ARGS+=" --uninstall $PKG"
		done
		rpm-ostree ${ARGS}
	else
		echo -e "Invalid options: $@" && exit 1
	fi
	`
)

func init() {
	cmdSwitchKernel.Flags().StringVar(&rtKernelRpmDir, "kernel-rt", "", "Path to kernel rt rpm directory")
	err := cmdSwitchKernel.MarkFlagRequired("kernel-rt")
	if err != nil {
		panic(err)
	}
	root.AddCommand(cmdSwitchKernel)
}

func runSwitchKernel(cmd *cobra.Command, args []string) error {
	// currently only supports RHCOS
	if kola.Options.Distribution != "rhcos" {
		return fmt.Errorf("Only supports `rhcos`")
	}

	var userdata *conf.UserData = conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "2.2.0"
		},
		"storage": {
			"files": [
				{
					"filesystem": "root",
					"path": "/var/home/core/switch-kernel.sh",
					"contents": {
						"source": "data:text/plain;base64,%s"
					},
					"mode": 110
				}
			]
		}
	}`, base64.StdEncoding.EncodeToString([]byte(switchKernelScript))))

	flight, err := kola.NewFlight(kolaPlatform)
	if err != nil {
		return errors.Wrapf(err, "failed to create new flight")
	}
	defer flight.Destroy()

	c, err := flight.NewCluster(&platform.RuntimeConfig{})
	if err != nil {
		return errors.Wrapf(err, "failed to create new cluster")
	}
	defer c.Destroy()

	m, err := c.NewMachine(userdata)
	if err != nil {
		return errors.Wrapf(err, "failed to spawn new machine")
	}
	defer m.Destroy()

	err = testSwitchKernel(c)
	if err != nil {
		return errors.Wrapf(err, "failed switch kernel test")
	}

	return nil
}

// Drops Kernel RT RPM files under the directory `localPath` to `$homeDir/kernel-rt-rpms` directory in m
func dropRpmFilesAll(m platform.Machine, localPath string) error {
	fmt.Println("Dropping RT Kernel RPMs...")
	re := regexp.MustCompile(`^kernel-rt-.*\.rpm$`)
	files, err := ioutil.ReadDir(localPath)
	if err != nil {
		return err
	}
	for _, f := range files {
		filename := f.Name()
		filepath := strings.TrimSuffix(localPath, "/") + "/" + filename
		targetPath := fmt.Sprintf("%s/kernel-rt-rpms/%s", homeDir, filename)
		if re.MatchString(filename) {
			in, err := os.Open(filepath)
			if err != nil {
				return err
			}
			if err := platform.InstallFile(in, m, targetPath); err != nil {
				return errors.Wrapf(err, "failed to upload %s", filename)
			}
		}
	}
	return nil
}

func switchDefaultToRtKernel(c platform.Cluster, m platform.Machine) error {
	// run the script to switch from default kernel to rt kernel
	fmt.Println("Switching from Default to RT Kernel...")
	cmd := "sudo " + homeDir + "/switch-kernel.sh default rt-kernel " + homeDir + "/kernel-rt-rpms/"
	stdout, stderr, err := m.SSH(cmd)
	if err != nil {
		return errors.Wrapf(err, "failed to run %s", cmd)
	}
	fmt.Printf("%s\n", stderr)
	fmt.Printf("%s\n", stdout)

	// reboot the machine to switch kernel
	fmt.Println("Rebooting machine...")
	err = m.Reboot()
	if err != nil {
		return errors.Wrapf(err, "failed to reboot machine")
	}

	// check if the kernel has switched to rt kernel
	fmt.Println("Checking kernel type...")
	cmd = "uname -v | grep -q 'PREEMPT RT'"
	_, _, err = m.SSH(cmd)
	if err != nil {
		return errors.Wrapf(err, "failed to run %s", cmd)
	}
	fmt.Println("Switched to RT Kernel successfully!")

	return nil
}

func switchRtKernelToDefault(c platform.Cluster, m platform.Machine) error {
	// run the script to switch from rt kernel to default
	fmt.Println("Switching from RT to Default Kernel...")
	cmd := "sudo " + homeDir + "/switch-kernel.sh rt-kernel default"
	stdout, stderr, err := m.SSH(cmd)
	if err != nil {
		return errors.Wrapf(err, "failed to run %s", cmd)
	}
	fmt.Printf("%s\n", stderr)
	fmt.Printf("%s\n", stdout)

	// reboot the machine to switch kernel
	fmt.Println("Rebooting machine...")
	err = m.Reboot()
	if err != nil {
		return errors.Wrapf(err, "failed to reboot machine")
	}

	// check if the kernel has switched back to default kernel
	fmt.Println("Checking kernel type...")
	cmd = "uname -v | grep -qv 'PREEMPT RT'"
	_, _, err = m.SSH(cmd)
	if err != nil {
		return errors.Wrapf(err, "failed to run %s", cmd)
	}
	fmt.Println("Switched back to Default Kernel successfully!")

	return nil
}

func testSwitchKernel(c platform.Cluster) error {
	m := c.Machines()[0]

	err := dropRpmFilesAll(m, rtKernelRpmDir)
	if err != nil {
		return errors.Wrapf(err, "failed to drop Kernel RT RPM files")
	}

	err = switchDefaultToRtKernel(c, m)
	if err != nil {
		return errors.Wrapf(err, "failed to switch from Default to RT Kernel")
	}

	err = switchRtKernelToDefault(c, m)
	if err != nil {
		return errors.Wrapf(err, "failed switching from RT to Default Kernel")
	}

	return nil
}
