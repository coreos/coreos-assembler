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

package misc

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

var (
	urlsToFetch = []string{
		"https://www.example.com/",
		"https://www.wikipedia.org/",
		"https://start.fedoraproject.org/",
	}
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         TestTLSFetchURLs,
		ClusterSize: 1,
		Name:        "coreos.tls.fetch-urls",
		Flags:       []register.Flag{register.RequiresInternetAccess}, // Networking outside cluster required
	})
}

func TestTLSFetchURLs(c cluster.TestCluster) {
	m := c.Machines()[0]

	for _, url := range urlsToFetch {
		c.RunCmdSyncf(m, "curl -s -S -m 30 --retry 2 %s", url)
	}
}
