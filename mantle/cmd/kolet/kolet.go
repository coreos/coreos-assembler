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
	"os"
	"strings"
	"syscall"
	"time"

	systemddbus "github.com/coreos/go-systemd/v22/dbus"
	systemdjournal "github.com/coreos/go-systemd/v22/journal"
	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola/register"

	// Register any tests that we may wish to execute in kolet.
	_ "github.com/coreos/mantle/kola/registry"
)

const (
	// From /usr/include/bits/siginfo-consts.h
	CLD_EXITED int32 = 1
	CLD_KILLED int32 = 2
)

// kolaRebootStamp should be created by tests that want to reboot
const kolaRebootStamp = "/run/kola-reboot"

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kolet")

	root = &cobra.Command{
		Use:   "kolet run [test] [func]",
		Short: "Native code runner for kola",
		Run:   run,
	}

	cmdRun = &cobra.Command{
		Use:   "run [test] [func]",
		Short: "Run a given test's native function",
		Run:   run,
	}

	cmdRunExtUnit = &cobra.Command{
		Use:          "run-test-unit [unitname]",
		Short:        "Monitor execution of a systemd unit",
		RunE:         runExtUnit,
		SilenceUsage: true,
	}
)

func run(cmd *cobra.Command, args []string) {
	cmd.Usage()
	os.Exit(2)
}

func registerTestMap(m map[string]*register.Test) {
	for testName, testObj := range m {
		if len(testObj.NativeFuncs) == 0 {
			continue
		}
		testCmd := &cobra.Command{
			Use: testName + " [func]",
			Run: run,
		}
		for nativeName := range testObj.NativeFuncs {
			nativeFuncWrap := testObj.NativeFuncs[nativeName]
			nativeRun := func(cmd *cobra.Command, args []string) {
				if len(args) != 0 {
					cmd.Usage()
					os.Exit(2)
				}
				if err := nativeFuncWrap.NativeFunc(); err != nil {
					plog.Fatal(err)
				}
				// Explicitly exit successfully.
				os.Exit(0)
			}
			nativeCmd := &cobra.Command{
				Use: nativeName,
				Run: nativeRun,
			}
			testCmd.AddCommand(nativeCmd)
		}
		cmdRun.AddCommand(testCmd)
	}
}

// requestRebootAndWait sends SIGTERM to the current process,
// which then propagates back to the ssh
// status, so that the kola runner (on the remote host)
// can use that as a trigger to reboot.
func requestRebootAndWait() error {
	selfproc := os.Process{
		Pid: os.Getpid(),
	}
	selfproc.Signal(syscall.SIGTERM)
	time.Sleep(time.Hour)
	panic("failed to send SIGTERM to self")
}

// dispatchRunExtUnit returns true if unit completed successfully, false if
// it's still running (or unit was terminated by SIGTERM)
func dispatchRunExtUnit(unitname string, sdconn *systemddbus.Conn) (bool, error) {
	props, err := sdconn.GetAllProperties(unitname)
	if err != nil {
		return false, errors.Wrapf(err, "listing unit properties")
	}

	result := props["Result"]
	if result == "exit-code" {
		return false, fmt.Errorf("Unit %s exited with code %d", unitname, props["ExecMainStatus"])
	}

	state := props["ActiveState"]
	substate := props["SubState"]

	switch state {
	case "inactive":
		fmt.Printf("Starting %s\n", unitname)
		sdconn.StartUnit(unitname, "fail", nil)
		return false, nil
	case "activating":
		return false, nil
	case "active":
		{
			switch substate {
			case "exited":
				maincode := props["ExecMainCode"]
				mainstatus := props["ExecMainStatus"]
				switch maincode {
				case CLD_EXITED:
					if mainstatus == int32(0) {
						_, err := os.Stat(kolaRebootStamp)
						if err == nil {
							systemdjournal.Print(systemdjournal.PriInfo, "Unit %s requested reboot via %s\n", unitname, kolaRebootStamp)
							return false, requestRebootAndWait()
						}
						return true, nil
					} else {
						// I don't think this can happen, we'd have exit-code above; but just
						// for completeness
						return false, fmt.Errorf("Unit %s failed with code %d", unitname, mainstatus)
					}
				case CLD_KILLED:
					// SIGTERM; we explicitly allow that, expecting we're rebooting.
					if mainstatus == int32(15) {
						systemdjournal.Print(systemdjournal.PriInfo, "Unit %s terminated via SIGTERM, assuming reboot request\n", unitname)
						return false, requestRebootAndWait()
					} else {
						return true, fmt.Errorf("Unit %s killed by signal %d", unitname, mainstatus)
					}
				default:
					return false, fmt.Errorf("Unit %s had unhandled code %d", unitname, maincode)
				}
			case "running":
				return false, nil
			case "failed":
				return true, fmt.Errorf("Unit %s in substate 'failed'", unitname)
			default:
				// Pass through other states
				return false, nil
			}
		}
	default:
		return false, fmt.Errorf("Unhandled systemd unit state:%s", state)
	}
}

func runExtUnit(cmd *cobra.Command, args []string) error {
	unitname := args[0]
	// Restrict this to services, don't need to support anything else right now
	if !strings.HasSuffix(unitname, ".service") {
		unitname = unitname + ".service"
	}
	sdconn, err := systemddbus.NewSystemConnection()
	if err != nil {
		return errors.Wrapf(err, "systemd connection")
	}
	if err := sdconn.Subscribe(); err != nil {
		return err
	}
	dispatchRunExtUnit(unitname, sdconn)
	unitevents, uniterrs := sdconn.SubscribeUnits(time.Second)

	for {
		select {
		case m := <-unitevents:
			for n := range m {
				if n == unitname {
					r, err := dispatchRunExtUnit(unitname, sdconn)
					if err != nil {
						return err
					}
					if r {
						return nil
					}
				}
			}
		case m := <-uniterrs:
			return m
		}
	}
}

func main() {
	registerTestMap(register.Tests)
	registerTestMap(register.UpgradeTests)
	root.AddCommand(cmdRun)
	root.AddCommand(cmdRunExtUnit)

	cli.Execute(root)
}
