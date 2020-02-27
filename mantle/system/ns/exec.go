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

package ns

import (
	"github.com/vishvananda/netns"

	"github.com/coreos/mantle/system/exec"
)

type Cmd struct {
	*exec.ExecCmd
	NsHandle netns.NsHandle
}

func Command(ns netns.NsHandle, name string, arg ...string) *Cmd {
	return &Cmd{
		ExecCmd:  exec.Command(name, arg...),
		NsHandle: ns,
	}
}

func (cmd *Cmd) CombinedOutput() ([]byte, error) {
	nsExit, err := Enter(cmd.NsHandle)
	if err != nil {
		return nil, err
	}
	defer nsExit()

	return cmd.ExecCmd.CombinedOutput()
}

func (cmd *Cmd) Output() ([]byte, error) {
	nsExit, err := Enter(cmd.NsHandle)
	if err != nil {
		return nil, err
	}
	defer nsExit()

	return cmd.ExecCmd.Output()
}

func (cmd *Cmd) Run() error {
	nsExit, err := Enter(cmd.NsHandle)
	if err != nil {
		return err
	}
	defer nsExit()

	return cmd.ExecCmd.Run()
}

func (cmd *Cmd) Start() error {
	nsExit, err := Enter(cmd.NsHandle)
	if err != nil {
		return err
	}
	defer nsExit()

	return cmd.ExecCmd.Start()
}
