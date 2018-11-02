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
	"github.com/coreos/go-omaha/omaha"

	"github.com/coreos/mantle/lang/destructor"
	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/network/ntp"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/system/ns"
)

type LocalFlight struct {
	destructor.MultiDestructor
	*platform.BaseFlight
}

func NewLocalFlight(opts *platform.Options, platformName platform.Name) (*LocalFlight, error) {
	bf, err := platform.NewBaseFlight(opts, platformName, "")
	if err != nil {
		return nil, err
	}

	lf := &LocalFlight{
		BaseFlight: bf,
	}
	lf.AddDestructor(lf.BaseFlight)

	return lf, nil
}

func (lf *LocalFlight) NewCluster(rconf *platform.RuntimeConfig) (*LocalCluster, error) {
	lc := &LocalCluster{
		flight: lf,
	}

	var err error
	lc.nshandle, err = ns.Create()
	if err != nil {
		return nil, err
	}
	lc.AddCloser(&lc.nshandle)

	nsdialer := network.NewNsDialer(lc.nshandle)
	lc.BaseCluster, err = platform.NewBaseClusterWithDialer(lf.BaseFlight, rconf, nsdialer)
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.AddDestructor(lc.BaseCluster)

	// dnsmasq and etcd much be launched in the new namespace
	nsExit, err := ns.Enter(lc.nshandle)
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	defer nsExit()

	lc.Dnsmasq, err = NewDnsmasq()
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.AddDestructor(lc.Dnsmasq)

	lc.SimpleEtcd, err = NewSimpleEtcd()
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.AddDestructor(lc.SimpleEtcd)

	lc.NTPServer, err = ntp.NewServer(":123")
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.AddCloser(lc.NTPServer)
	go lc.NTPServer.Serve()

	omahaServer, err := omaha.NewTrivialServer(":34567")
	if err != nil {
		lc.Destroy()
		return nil, err
	}
	lc.OmahaServer = OmahaWrapper{TrivialServer: omahaServer}
	lc.AddDestructor(lc.OmahaServer)
	go lc.OmahaServer.Serve()

	// does not lf.AddCluster() since we are not the top-level object

	return lc, nil
}

func (lf *LocalFlight) Destroy() {
	lf.MultiDestructor.Destroy()
}
