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

package etcd

import (
	"fmt"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/etcd")

func init() {
	// test etcd discovery with 0.4.7
	register.Register(&register.Test{
		Run:         DiscoveryV1,
		Manual:      true,
		ClusterSize: 3,
		Name:        "coreos.etcd0.discovery",
		UserData: `#cloud-config
coreos:
  etcd:
    name: $name
    discovery: $discovery
    addr: $private_ipv4:2379
    peer-addr: $private_ipv4:2380`,
	})

	// test etcd discovery with 2.0 with new cloud config
	register.Register(&register.Test{
		Run:         DiscoveryV2,
		ClusterSize: 3,
		Name:        "coreos.etcd2.discovery",
		UserData: `#cloud-config

coreos:
  etcd2:
    name: $name
    discovery: $discovery
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
    listen-peer-urls: http://$private_ipv4:2380,http://$private_ipv4:7001`,
	})
}

func DiscoveryV2(c platform.TestCluster) error {
	return discovery(c, 2)
}

func DiscoveryV1(c platform.TestCluster) error {
	return discovery(c, 1)
}

func doStart(m platform.Machine, version int, block bool) error {
	// start etcd instance
	var etcdStart string
	if version == 1 {
		etcdStart = "sudo systemctl start etcd.service"
	} else if version == 2 {
		etcdStart = "sudo systemctl start etcd2.service"
	} else {
		return fmt.Errorf("etcd version unspecified")
	}

	if !block {
		etcdStart += " --no-block"
	}

	_, err := m.SSH(etcdStart)
	if err != nil {
		return fmt.Errorf("SSH cmd to %v failed: %s", m.IP(), err)
	}

	return nil
}

func discovery(cluster platform.Cluster, version int) error {
	if plog.LevelAt(capnslog.DEBUG) {
		// get journalctl -f from all machines before starting
		for _, m := range cluster.Machines() {
			if err := platform.StreamJournal(m); err != nil {
				return fmt.Errorf("failed to start journal: %v", err)
			}
		}
	}

	// start etcd on each machine asynchronously.
	for _, m := range cluster.Machines() {
		if err := doStart(m, version, false); err != nil {
			return err
		}
	}

	// block until each instance is reported as started.
	for i, m := range cluster.Machines() {
		if err := doStart(m, version, true); err != nil {
			return err
		}
		plog.Infof("etcd instance%d started", i)
	}

	// NOTE(pb): this check makes the next code somewhat redundant
	if err := GetClusterHealth(cluster.Machines()[0], len(cluster.Machines())); err != nil {
		return fmt.Errorf("discovery failed cluster-health check: %v", err)
	}

	var keyMap map[string]string
	var retryFuncs []func() error

	retryFuncs = append(retryFuncs, func() error {
		var err error
		keyMap, err = setKeys(cluster, 5)
		if err != nil {
			return err
		}
		return nil
	})
	retryFuncs = append(retryFuncs, func() error {
		var quorumRead bool
		if version == 2 {
			quorumRead = true
		}
		if err := checkKeys(cluster, keyMap, quorumRead); err != nil {
			return err
		}
		return nil
	})
	for _, retry := range retryFuncs {
		if err := util.Retry(5, 5*time.Second, retry); err != nil {
			return fmt.Errorf("discovery failed set/get check: %v", err)
		}
		// NOTE(pb): etcd1 seems to fail in an odd way when I try quorum
		// read, instead just sleep between setting and getting.
		time.Sleep(2 * time.Second)
	}

	return nil
}
