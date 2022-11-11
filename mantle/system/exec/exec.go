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

// exec is extension of the standard os.exec package.
// Adds a handy dandy interface and assorted other features.
package exec

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

var (
	// for equivalence with os/exec
	ErrNotFound = exec.ErrNotFound
	LookPath    = exec.LookPath
)

// An exec.Cmd compatible interface.
type Cmd interface {
	// Methods provided by exec.Cmd
	CombinedOutput() ([]byte, error)
	Output() ([]byte, error)
	Run() error
	Start() error
	StderrPipe() (io.ReadCloser, error)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	Wait() error

	// Simplified wrapper for Process.Kill + Wait
	Kill() error

	// Simplified wrapper for Process.Pid
	Pid() int

	// Simplified wrapper to know if a process was signaled
	Signaled() bool
}

// Basic Cmd implementation based on exec.Cmd
type ExecCmd struct {
	*exec.Cmd
	cancel context.CancelFunc
	wait   sync.Once
}

func Command(name string, arg ...string) *ExecCmd {
	return CommandContext(context.Background(), name, arg...)
}

func CommandContext(ctx context.Context, name string, arg ...string) *ExecCmd {
	ctx, cancel := context.WithCancel(ctx)
	return &ExecCmd{
		Cmd:    exec.CommandContext(ctx, name, arg...),
		cancel: cancel,
	}
}

func (cmd *ExecCmd) Wait() error {
	var err error
	cmd.wait.Do(func() {
		err = cmd.Cmd.Wait()
	})
	return err
}

// safe even if already dead
func (cmd *ExecCmd) Kill() error {
	cmd.cancel()
	err := cmd.Wait()
	if err == nil {
		return nil
	}

	if eerr, ok := err.(*exec.ExitError); ok {
		status := eerr.Sys().(syscall.WaitStatus)
		if status.Signal() == syscall.SIGKILL {
			return nil
		}
	}
	return err
}

func (cmd *ExecCmd) Signaled() bool {
	if cmd.ProcessState == nil {
		return false
	}
	status := cmd.ProcessState.Sys().(syscall.WaitStatus)
	return status.Signaled()
}

func (cmd *ExecCmd) Pid() int {
	return cmd.Process.Pid
}

// IsCmdNotFound reports true if the underlying error was exec.ErrNotFound.
func IsCmdNotFound(err error) bool {
	if eerr, ok := err.(*exec.Error); ok && eerr.Err == ErrNotFound {
		return true
	}
	return false
}
