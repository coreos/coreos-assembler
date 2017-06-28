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

// inspired by github.com/docker/docker/pkg/reexec

package exec

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// prefix of first argument if it is defining an entrypoint to be called.
const entryArgPrefix = "_MULTICALL_ENTRYPOINT_"

var exePath string

func init() {
	// save the program path
	var err error
	exePath, err = os.Readlink("/proc/self/exe")
	if err != nil {
		panic("cannot get current executable")
	}
}

type entrypointFn func(args []string) error

var entrypoints = make(map[string]entrypointFn)

// Entrypoint provides the access to a multicall command.
type Entrypoint string

// NewEntrypoint adds a new multicall command. name is the command name
// and fn is the function that will be executed for the specified
// command. It returns the related Entrypoint. Packages adding new
// multicall commands should call Add in their init function.
func NewEntrypoint(name string, fn entrypointFn) Entrypoint {
	if _, ok := entrypoints[name]; ok {
		panic(fmt.Errorf("command with name %q already exists", name))
	}
	entrypoints[name] = fn
	return Entrypoint(name)
}

// MaybeExec should be called at the start of the program, if the process argv[0] is
// a name registered with multicall, the related function will be executed.
// If the functions returns an error, it will be printed to stderr and will
// exit with an exit status of 1, otherwise it will exit with a 0 exit status.
func MaybeExec() {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], entryArgPrefix) {
		return
	}
	name := os.Args[1][len(entryArgPrefix):]
	if err := entrypoints[name](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Command will prepare the *ExecCmd for the given entrypoint, configured with
// the provided args.
func (e Entrypoint) Command(args ...string) *ExecCmd {
	args = append([]string{entryArgPrefix + string(e)}, args...)
	cmd := Command(exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	return cmd
}

// Sudo will prepare the *ExecCmd for the given entrypoint to be run as root
// via sudo with the provided args.
func (e Entrypoint) Sudo(args ...string) *ExecCmd {
	args = append([]string{"-E", "-p", "sudo password for %p: ", "--",
		exePath, entryArgPrefix + string(e)}, args...)
	cmd := Command("sudo", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	return cmd
}
