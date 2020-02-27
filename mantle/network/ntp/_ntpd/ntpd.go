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

package main

import (
	"flag"
	"os"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/network/ntp"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "main")
	now  = flag.String("now", "", "Internal time for the server.")
	leap = flag.String("leap", "", "Handle a leap second.")
)

func main() {
	flag.Parse()
	capnslog.SetFormatter(capnslog.NewStringFormatter(os.Stderr))
	capnslog.SetGlobalLogLevel(capnslog.INFO)

	var l, n time.Time
	var err error
	if *now != "" {
		n, err = time.Parse(time.UnixDate, *now)
		if err != nil {
			plog.Fatalf("Parsing --now failed: %v", err)
		}
	}
	if *leap != "" {
		l, err = time.Parse(time.UnixDate, *leap)
		if err != nil {
			plog.Fatalf("Parsing --leap failed: %v", err)
		}
		if (l.Truncate(24*time.Hour) != l) || (l.UTC().Day() != 1) {
			plog.Fatalf("Invalid --leap time: %s", l)
		}
	}

	s, err := ntp.NewServer(":123")
	if err != nil {
		plog.Fatalf("Listen failed: %v", err)
	}

	if !n.IsZero() {
		s.SetTime(n)
	}
	if !l.IsZero() {
		s.SetLeapSecond(l, ntp.LEAP_ADD)
	}

	s.Serve()
}
