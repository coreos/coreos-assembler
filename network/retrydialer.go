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
	"time"
)

const (
	// DefaultTimeout sets the default timeout for RetryDialer.
	DefaultTimeout = 5 * time.Second

	// DefaultKeepAlive sets the default keepalive for RetryDialer.
	DefaultKeepAlive = 30 * time.Second

	// DefaultRetries sets the default number of retries for RetryDialer.
	DefaultRetries = 7
)

// RetryDialer is intended to timeout quickly and retry connecting instead
// of just failing. Particularly useful for waiting on a booting machine.
type RetryDialer struct {
	net.Dialer
	Retries int
}

// NewRetryDialer initializes a RetryDialer with reasonable default settings.
func NewRetryDialer() *RetryDialer {
	return &RetryDialer{
		Dialer: net.Dialer{
			Timeout:   DefaultTimeout,
			KeepAlive: DefaultKeepAlive,
		},
		Retries: DefaultRetries,
	}
}

// Dial connects to a remote address, retrying on failure.
func (d *RetryDialer) Dial(network, address string) (c net.Conn, err error) {
	for i := 0; i < d.Retries; i++ {
		c, err = d.Dialer.Dial(network, address)
		if err == nil {
			return
		}
	}
	return
}
