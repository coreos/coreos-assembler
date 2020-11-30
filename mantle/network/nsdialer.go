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

package network

import (
	"net"

	"github.com/vishvananda/netns"

	"github.com/coreos/mantle/system/ns"
)

// NsDialer is a RetryDialer that can enter any network namespace.
type NsDialer struct {
	RetryDialer
	NsHandle netns.NsHandle
}

func NewNsDialer(ns netns.NsHandle) *NsDialer {
	return &NsDialer{
		RetryDialer: RetryDialer{
			Dialer: net.Dialer{
				Timeout:   DefaultTimeout,
				KeepAlive: DefaultKeepAlive,
			},
			Retries: DefaultRetries,
		},
		NsHandle: ns,
	}
}

func (d *NsDialer) Dial(network, address string) (net.Conn, error) {
	nsExit, err := ns.Enter(d.NsHandle)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = nsExit()
	}()

	return d.RetryDialer.Dial(network, address)
}
