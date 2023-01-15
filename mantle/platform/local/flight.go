// Copyright 2015 CoreOS, Inc.
// Copyright 2018 Red Hat
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

	"github.com/coreos/coreos-assembler/mantle/lang/destructor"
	"github.com/coreos/coreos-assembler/mantle/network"
	"github.com/coreos/coreos-assembler/mantle/network/ntp"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/system/ns"
)

const (
	listenPortBase = 30000
)

type LocalFlight struct {
	destructor.MultiDestructor
	*platform.BaseFlight
	Dnsmasq    *Dnsmasq
	NTPServer  *ntp.Server
	nshandle   netns.NsHandle
	listenPort int32
}

func NewLocalFlight(opts *platform.Options, platformName platform.Name) (*LocalFlight, error) {
	nshandle, err := ns.Create()
	if err != nil {
		return nil, err
	}

	nsdialer := network.NewNsDialer(nshandle)
	bf, err := platform.NewBaseFlightWithDialer(opts, platformName, nsdialer)
	if err != nil {
		nshandle.Close()
		return nil, err
	}

	lf := &LocalFlight{
		BaseFlight: bf,
		nshandle:   nshandle,
		listenPort: listenPortBase,
	}
	lf.AddDestructor(lf.BaseFlight)
	lf.AddCloser(&lf.nshandle)

	// dnsmasq and etcd must be launched in the new namespace
	nsExit, err := ns.Enter(lf.nshandle)
	if err != nil {
		lf.Destroy()
		return nil, err
	}
	defer func() {
		_ = nsExit()
	}()

	lf.Dnsmasq, err = NewDnsmasq()
	if err != nil {
		lf.Destroy()
		return nil, err
	}
	lf.AddDestructor(lf.Dnsmasq)

	lf.NTPServer, err = ntp.NewServer(":123")
	if err != nil {
		lf.Destroy()
		return nil, err
	}
	lf.AddCloser(lf.NTPServer)
	go lf.NTPServer.Serve()

	return lf, nil
}

func (lf *LocalFlight) NewCluster(rconf *platform.RuntimeConfig) (*LocalCluster, error) {
	lc := &LocalCluster{
		flight: lf,
	}

	var err error
	lc.BaseCluster, err = platform.NewBaseCluster(lf.BaseFlight, rconf)
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.AddDestructor(lc.BaseCluster)

	// does not lf.AddCluster() since we are not the top-level object

	return lc, nil
}

func (lf *LocalFlight) Destroy() {
	lf.MultiDestructor.Destroy()
}
