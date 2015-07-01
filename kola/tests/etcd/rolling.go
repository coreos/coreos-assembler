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
	"path/filepath"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/platform"
)

const (
	etcdVersion  = "etcd 2.0.12"
	etcdVersion2 = `{"etcdserver":"2.1.0-alpha.1","etcdcluster":"2.1.0"}`
	etcdBin      = "./etcd"
	etcdBin2     = "./etcd2"
	dropPath     = "/home/core"
	settingSize  = 20 // number of random keys set and checked per node multiple times
)

func RollingUpgrade(cluster platform.TestCluster) error {
	csize := len(cluster.Machines())

	if plog.LevelAt(capnslog.DEBUG) {
		// get journalctl -f from all machines before starting
		for _, m := range cluster.Machines() {
			if err := m.StartJournal(); err != nil {
				return fmt.Errorf("failed to start journal: %v", err)
			}
		}
	}

	// drop in starting etcd binary
	plog.Debug("adding files to cluster")
	if err := cluster.DropFile(etcdBin); err != nil {
		return err
	}
	// TODO(pb): skip this test if binaries aren't available once we
	// have meaninful way to do so.

	// drop in etcd binary to upgrade to
	if err := cluster.DropFile(etcdBin2); err != nil {
		return err
	}

	// replace existing etcd2 binary with 2.0.12
	plog.Info("replacing etcd with 2.0.12")
	etcdPath := filepath.Join(dropPath, filepath.Base(etcdBin))
	for _, m := range cluster.Machines() {
		if err := replaceEtcd2Bin(m, etcdPath); err != nil {
			return err
		}
	}

	// start 2.0 cluster
	plog.Info("starting 2.0 cluster")
	for _, m := range cluster.Machines() {
		if err := startEtcd2(m); err != nil {
			return err
		}
	}
	for _, m := range cluster.Machines() {
		if err := getClusterHealth(m, csize); err != nil {
			return err
		}
	}
	for _, m := range cluster.Machines() {
		if err := checkEtcdVersion(cluster, m, etcdVersion); err != nil {
			return err
		}
	}

	// set some values on all nodes
	mapSet, err := setKeys(cluster, settingSize)
	if err != nil {
		return err
	}

	// rolling replacement checking cluster health, and
	// version after each replaced binary. Also test
	plog.Info("rolling upgrade to 2.1")
	etcdPath2 := filepath.Join(dropPath, filepath.Base(etcdBin2))
	for i, m := range cluster.Machines() {

		// check current value set
		if err := checkKeys(cluster, mapSet); err != nil {
			return err
		}

		plog.Infof("stopping instance %v", i)
		if err := stopEtcd2(m); err != nil {
			return err
		}
		if err := replaceEtcd2Bin(m, etcdPath2); err != nil {
			return err
		}

		// set some values while running down a node and update set
		tempSet, err := setKeys(cluster, settingSize)
		if err != nil {
			return err
		}
		mapCopy(mapSet, tempSet)

		plog.Infof("starting instance %v with upgraded binary", i)
		if err := startEtcd2(m); err != nil {
			return err
		}

		for _, m := range cluster.Machines() {
			if err := getClusterHealth(m, csize); err != nil {
				return err
			}
		}

	}
	// set some more values
	tempSet, err := setKeys(cluster, settingSize)
	if err != nil {
		return err
	}
	mapCopy(mapSet, tempSet)

	// final check all values written correctly
	if err := checkKeys(cluster, mapSet); err != nil {
		return err
	}

	// check version is now 2.1
	for _, m := range cluster.Machines() {
		if err := checkEtcdVersion(cluster, m, etcdVersion2); err != nil {
			return err
		}
	}

	return nil
}

// copies m2 into m1 overwriting any overlapping keys
func mapCopy(m1, m2 map[string]string) {
	for k, v := range m2 {
		m1[k] = v
	}
}
