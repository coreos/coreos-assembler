/*
   Copyright 2015 CoreOS, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package local

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/coreos/coreos-cloudinit/config"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/coreos/mantle/network"
)

type LocalCluster struct {
	SSHAgent    *network.SSHAgent
	Dnsmasq     *Dnsmasq
	ConfigDrive *ConfigDrive
	nshandle    netns.NsHandle
}

func NewLocalCluster() (*LocalCluster, error) {
	// Before creating a new namespace lock to a thread and restore the
	// original namespace on return to avoid confusing other goroutines.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return nil, err
	}
	defer netns.Set(origns)

	lc := &LocalCluster{}
	lc.nshandle, err = netns.New()
	if err != nil {
		return nil, err
	}

	dialer := NewNsDialer(lc.nshandle)
	lc.SSHAgent, err = network.NewSSHAgent(dialer)
	if err != nil {
		lc.nshandle.Close()
		return nil, err
	}

	cfgData := config.CloudConfig{}
	err = lc.SSHAgent.UpdateConfig(&cfgData)
	if err != nil {
		lc.nshandle.Close()
		return nil, err
	}

	lc.ConfigDrive, err = NewConfigDrive(&cfgData)
	if err != nil {
		lc.nshandle.Close()
		return nil, err
	}

	lc.Dnsmasq, err = NewDnsmasq()
	if err != nil {
		lc.nshandle.Close()
		lc.ConfigDrive.Destroy()
		return nil, err
	}

	return lc, nil
}

func (lc *LocalCluster) CommandStart(cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return err
	}
	defer netns.Set(origns)

	err = netns.Set(lc.nshandle)
	if err != nil {
		return err
	}

	return cmd.Start()
}

func (lc *LocalCluster) NewTap(bridge string) (*TunTap, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return nil, err
	}
	defer netns.Set(origns)

	err = netns.Set(lc.nshandle)
	if err != nil {
		return nil, err
	}

	tap, err := AddLinkTap("")
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

func (lc *LocalCluster) Destroy() error {
	lc.Dnsmasq.Destroy()
	lc.nshandle.Close()
	return nil
}
