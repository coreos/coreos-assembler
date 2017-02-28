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

package exec

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func TestExecCmdKill(t *testing.T) {
	cmd := Command("sleep", "3600")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := cmd.Kill(); err != nil {
		t.Errorf("Kill failed: %v", err)
	}

	if cmd.ProcessState == nil {
		t.Fatalf("ProcessState is nil")
	}

	status := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if status.Signal() != syscall.SIGKILL {
		t.Errorf("Unexpected state: %s", cmd.ProcessState)
	}
}

func TestExecCmdCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "sleep", "3600")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()
	if err := cmd.Wait(); err == nil {
		t.Errorf("Killed without an error")
	} else if state, ok := err.(*exec.ExitError); ok {
		status := state.Sys().(syscall.WaitStatus)
		if status.Signal() != syscall.SIGKILL {
			t.Errorf("Unexpected state: %s", state)
		}
	} else {
		t.Errorf("Unexpected error: %v", err)
	}
}
