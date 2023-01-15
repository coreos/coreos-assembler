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
	"fmt"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/coreos/coreos-assembler/mantle/lang/destructor"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/system/exec"
	"github.com/coreos/coreos-assembler/mantle/system/ns"
)

type LocalCluster struct {
	destructor.MultiDestructor
	*platform.BaseCluster
	flight *LocalFlight
}

func (lc *LocalCluster) NewCommand(name string, arg ...string) exec.Cmd {
	cmd := ns.Command(lc.flight.nshandle, name, arg...)
	return cmd
}

func (lc *LocalCluster) NewTap(bridge string) (tap *TunTap, err error) {
	nsExit, err := ns.Enter(lc.flight.nshandle)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = nsExit()
	}()

	tap, err = AddLinkTap("")
	if err != nil {
		return nil, fmt.Errorf("tap failed: %v", err)
	}

	err = netlink.LinkSetUp(tap)
	if err != nil {
		return nil, fmt.Errorf("tap up failed: %v", err)
	}

	br, err := netlink.LinkByName(bridge)
	if err != nil {
		return nil, fmt.Errorf("bridge failed: %v", err)
	}

	err = netlink.LinkSetMaster(tap, br.(*netlink.Bridge))
	if err != nil {
		return nil, fmt.Errorf("set master failed: %v", err)
	}

	return tap, nil
}

func (lc *LocalCluster) GetNsHandle() netns.NsHandle {
	return lc.flight.nshandle
}

func (lc *LocalCluster) Destroy() {
	// does not lc.flight.DelCluster() since we are not the top-level object
	lc.MultiDestructor.Destroy()
}
