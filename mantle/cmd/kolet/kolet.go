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
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	systemddbus "github.com/coreos/go-systemd/v22/dbus"
	systemdjournal "github.com/coreos/go-systemd/v22/journal"
	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/register"

	// Register any tests that we may wish to execute in kolet.
	_ "github.com/coreos/mantle/kola/registry"
)

const (
	// From /usr/include/bits/siginfo-consts.h
	CLD_EXITED int32 = 1
	CLD_KILLED int32 = 2
)

// Reboot handling
// ---
//
// Rebooting is complicated!  The high level API we expose is the one defined by
// the Debian autopkgtest specification:
// https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst
//
// Today kola has support for rebooting a machine that ends up in a loop with SSH,
// checking the value of /proc/sys/kernel/random/boot_id.
// Originally we implemented the plain API that immediately starts a reboot by calling
// back to the harness API.  Now we want to support the `autopkgtest-reboot-prepare` API, so
// things are a bit more complicated, because the actual reboot is initiated by the client.
//
// The "immediate" reboot API is implemented in terms of the prepare API now.
//
// There are a few distinct actors here; using the term "subject" for the system
// under test:
//
// harness: The process running the coreos-assembler container
// login: The SSH login session initated on the subject (target) system
// unit: The systemd unit on the subject system running the test, currently named kola-runext.service
//
// We need to *synchronously* communicate state from the unit to back to the harness.  The
// login and unit also need to communicate to make this happen, because the channel
// between the harness and subject is SSH.
//
// To implement communication, we use a "low tech" method of two FIFOs. (If we need
// more sophistication in the future, it would probably make sense for the login
// session to expose an API over a unix domain socket).
//
// When a reboot binary is invoked, it creates the "reboot acknowledge" FIFO in /run,
// then writes the mark out to a second FIFO that the login session is waiting on.
// The reboot binary then *synchronously* waits on a read from FIFO to signal
// acknowledgement for rebooting.
//
// When the login session finds data in the "reboot request" FIFO, it reads it
// and then prints out the reboot data on stdout, which the harness reads.
//
// The harness then creates a separate SSH session which stops sshd (to avoid any races
// around logging in again), and then writes to the acknowlege FIFO,
// allowing the reboot binary to continue.
//
// At this point, the "mark" (or state saved between reboots) is safely on the harness,
// so the test code can invoke e.g. `reboot` or `reboot -ff` etc.
//
// The harness keeps polling via ssh, waiting until it can log in and also detects
// that the boot ID is different, and passes in the mark via an environment variable.

const (
	autopkgTestRebootPath   = "/tmp/autopkgtest-reboot"
	autopkgtestRebootScript = `#!/bin/bash
set -xeuo pipefail
~core/kolet reboot-request "$1"
reboot
`
	autopkgTestRebootPreparePath = "/tmp/autopkgtest-reboot-prepare"

	autopkgtestRebootPrepareScript = `#!/bin/bash
set -euo pipefail
exec ~core/kolet reboot-request "$1"
`

	// File used to communicate between the script and the kolet runner internally
	rebootRequestFifo = "/run/kolet-reboot"
)

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

	cmdReboot = &cobra.Command{
		Use:          "reboot-request MARK",
		Short:        "Request a reboot",
		RunE:         runReboot,
		SilenceUsage: true,
	}

	cmdHttpd = &cobra.Command{
		Use:   "httpd",
		Short: "Start an HTTP server to serve the contents of the file system",
		RunE:  runHttpd,
	}
)

func run(cmd *cobra.Command, args []string) {
	err := cmd.Usage()
	if err != nil {
		plog.Error(err)
	}
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
					if err := cmd.Usage(); err != nil {
						plog.Error(err)
					}
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
		_, err := sdconn.StartUnit(unitname, "fail", nil)
		return false, err
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
						return true, nil
					} else {
						// I don't think this can happen, we'd have exit-code above; but just
						// for completeness
						return false, fmt.Errorf("Unit %s failed with code %d", unitname, mainstatus)
					}
				case CLD_KILLED:
					return true, fmt.Errorf("Unit %s killed by signal %d", unitname, mainstatus)
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

func initiateReboot(mark string) error {
	systemdjournal.Print(systemdjournal.PriInfo, "Processing reboot request")
	res := kola.KoletResult{
		Reboot: string(mark),
	}
	buf, err := json.Marshal(&res)
	if err != nil {
		return errors.Wrapf(err, "serializing KoletResult")
	}
	fmt.Println(string(buf))
	systemdjournal.Print(systemdjournal.PriInfo, "Acknowledged reboot request with mark: %s", buf)
	return nil
}

func runExtUnit(cmd *cobra.Command, args []string) error {
	rebootOff, _ := cmd.Flags().GetBool("deny-reboots")
	// Write the autopkgtest wrappers
	if err := ioutil.WriteFile(autopkgTestRebootPath, []byte(autopkgtestRebootScript), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(autopkgTestRebootPreparePath, []byte(autopkgtestRebootPrepareScript), 0755); err != nil {
		return err
	}

	// Create the reboot cmdline -> login FIFO for the reboot mark and
	// proxy it into a channel
	rebootChan := make(chan string)
	errChan := make(chan error)

	// We want to prevent certain tests (like non-exclusive tests) from rebooting
	if !rebootOff {
		err := exec.Command("mkfifo", rebootRequestFifo).Run()
		if err != nil {
			return err
		}
		go func() {
			rebootReader, err := os.Open(rebootRequestFifo)
			if err != nil {
				errChan <- err
				return
			}
			defer rebootReader.Close()
			buf, err := ioutil.ReadAll(rebootReader)
			if err != nil {
				errChan <- err
			}
			rebootChan <- string(buf)
		}()
	}

	unitname := args[0]
	// Restrict this to services, don't need to support anything else right now
	if !strings.HasSuffix(unitname, ".service") {
		unitname = unitname + ".service"
	}
	sdconn, err := systemddbus.NewSystemConnection()
	if err != nil {
		return errors.Wrapf(err, "systemd connection")
	}

	// Start the unit; it's not started by default because we need to
	// do some preparatory work above (and some is done in the harness)
	if _, err := sdconn.StartUnit(unitname, "fail", nil); err != nil {
		return errors.Wrapf(err, "starting unit")
	}

	if err := sdconn.Subscribe(); err != nil {
		return err
	}
	// Check the status now to avoid any race conditions
	_, err = dispatchRunExtUnit(unitname, sdconn)
	if err != nil {
		return err
	}
	// Watch for changes in the target unit
	filterFunc := func(n string) bool {
		return n != unitname
	}
	compareFunc := func(u1, u2 *systemddbus.UnitStatus) bool { return *u1 != *u2 }
	unitevents, uniterrs := sdconn.SubscribeUnitsCustom(time.Second, 0, compareFunc, filterFunc)

	for {
		systemdjournal.Print(systemdjournal.PriInfo, "Awaiting events")
		select {
		case err := <-errChan:
			return err
		case reboot := <-rebootChan:
			return initiateReboot(reboot)
		case m := <-unitevents:
			for n := range m {
				if n == unitname {
					systemdjournal.Print(systemdjournal.PriInfo, "Dispatching %s", n)
					r, err := dispatchRunExtUnit(unitname, sdconn)
					systemdjournal.Print(systemdjournal.PriInfo, "Done dispatching %s", n)
					if err != nil {
						return err
					}
					if r {
						return nil
					}
				} else {
					systemdjournal.Print(systemdjournal.PriInfo, "Unexpected event %v", n)
				}
			}
		case m := <-uniterrs:
			return m
		}
	}
}

// This is a backend intending to support at least the same
// API as defined by Debian autopkgtests:
// https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst
func runReboot(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(rebootRequestFifo); os.IsNotExist(err) {
		return errors.New("Reboots are not supported for this test, rebootRequestFifo does not exist.")
	}

	mark := args[0]
	systemdjournal.Print(systemdjournal.PriInfo, "Requesting reboot with mark: %s", mark)
	err := exec.Command("mkfifo", kola.KoletRebootAckFifo).Run()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(rebootRequestFifo, []byte(mark), 0644)
	if err != nil {
		return err
	}
	f, err := os.Open(kola.KoletRebootAckFifo)
	if err != nil {
		return err
	}
	buf := make([]byte, 1)
	_, err = f.Read(buf)
	if err != nil {
		return err
	}
	systemdjournal.Print(systemdjournal.PriInfo, "Reboot request acknowledged")
	return nil
}

func runHttpd(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetString("port")
	path, _ := cmd.Flags().GetString("path")
	http.Handle("/", http.FileServer(http.Dir(path)))
	plog.Info("Starting HTTP server")
	return http.ListenAndServe(fmt.Sprintf("localhost:%s", port), nil)
}

func main() {
	registerTestMap(register.Tests)
	registerTestMap(register.UpgradeTests)
	root.AddCommand(cmdRun)
	cmdRunExtUnit.Flags().Bool("deny-reboots", false, "disable reboot requests")
	root.AddCommand(cmdRunExtUnit)
	cmdReboot.Args = cobra.ExactArgs(1)
	root.AddCommand(cmdReboot)
	cmdHttpd.Flags().StringP("port", "", "80", "port")
	cmdHttpd.Flags().StringP("path", "", "./", "path to filesystem contents to serve")
	cmdHttpd.Args = cobra.ExactArgs(0)
	root.AddCommand(cmdHttpd)

	cli.Execute(root)
}
