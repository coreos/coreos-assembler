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

package local

import (
	"github.com/vishvananda/netns"

	"github.com/coreos/mantle/system/exec"
)

type NsCmd struct {
	*exec.ExecCmd
	NsHandle netns.NsHandle
}

func NewNsCommand(ns netns.NsHandle, name string, arg ...string) *NsCmd {
	return &NsCmd{
		ExecCmd:  exec.Command(name, arg...),
		NsHandle: ns,
	}
}

func (cmd *NsCmd) CombinedOutput() ([]byte, error) {
	nsExit, err := NsEnter(cmd.NsHandle)
	if err != nil {
		return nil, err
	}
	defer nsExit()

	return cmd.ExecCmd.CombinedOutput()
}

func (cmd *NsCmd) Output() ([]byte, error) {
	nsExit, err := NsEnter(cmd.NsHandle)
	if err != nil {
		return nil, err
	}
	defer nsExit()

	return cmd.ExecCmd.Output()
}

func (cmd *NsCmd) Run() error {
	nsExit, err := NsEnter(cmd.NsHandle)
	if err != nil {
		return err
	}
	defer nsExit()

	return cmd.ExecCmd.Run()
}

func (cmd *NsCmd) Start() error {
	nsExit, err := NsEnter(cmd.NsHandle)
	if err != nil {
		return err
	}
	defer nsExit()

	return cmd.ExecCmd.Start()
}
