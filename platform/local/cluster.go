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
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/coreos/mantle/lang/destructor"
	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/network/ntp"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/ns"
)

type LocalCluster struct {
	destructor.MultiDestructor
	*platform.BaseCluster
	flight      *LocalFlight
	Dnsmasq     *Dnsmasq
	NTPServer   *ntp.Server
	OmahaServer OmahaWrapper
	SimpleEtcd  *SimpleEtcd
	nshandle    netns.NsHandle
}

func (lc *LocalCluster) NewCommand(name string, arg ...string) exec.Cmd {
	cmd := ns.Command(lc.nshandle, name, arg...)
	return cmd
}

func (lc *LocalCluster) hostIP() string {
	// hackydoo
	bridge := "br0"
	for _, seg := range lc.Dnsmasq.Segments {
		if bridge == seg.BridgeName {
			return seg.BridgeIf.DHCPv4[0].IP.String()
		}
	}
	panic("Not a valid bridge!")
}

func (lc *LocalCluster) etcdEndpoint() string {
	return fmt.Sprintf("http://%s:%d", lc.hostIP(), lc.SimpleEtcd.Port)
}

func (lc *LocalCluster) GetDiscoveryURL(size int) (string, error) {
	baseURL := fmt.Sprintf("%v/v2/keys/discovery/%v", lc.etcdEndpoint(), rand.Int())

	nsDialer := network.NewNsDialer(lc.nshandle)
	tr := &http.Transport{
		Dial: nsDialer.Dial,
	}
	client := &http.Client{Transport: tr}

	body := strings.NewReader(url.Values{"value": {strconv.Itoa(size)}}.Encode())
	req, err := http.NewRequest("PUT", baseURL+"/_config/size", body)
	if err != nil {
		return "", fmt.Errorf("setting discovery url failed: %v\n", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("setting discovery url failed: %v\n", err)
	}
	defer resp.Body.Close()

	return baseURL, nil
}

func (lc *LocalCluster) GetOmahaHostPort() (string, error) {
	_, port, err := net.SplitHostPort(lc.OmahaServer.Addr().String())
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(lc.hostIP(), port), nil
}

func (lc *LocalCluster) NewTap(bridge string) (*TunTap, error) {
	nsExit, err := ns.Enter(lc.nshandle)
	if err != nil {
		return nil, err
	}
	defer nsExit()

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

func (lc *LocalCluster) GetNsHandle() netns.NsHandle {
	return lc.nshandle
}

func (lc *LocalCluster) Destroy() {
	// does not lc.flight.DelCluster() since we are not the top-level object
	lc.MultiDestructor.Destroy()
}
