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
	"encoding/json"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/etcd")

func init() {
	register.Register(&register.Test{
		Run:         Discovery,
		ClusterSize: 3,
		Name:        "cl.etcd-member.discovery",
		UserData: conf.Ignition(`{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd-member.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/lib/coreos/etcd-wrapper --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379 --listen-peer-urls=http://$private_ipv4:2380"
        }]
      }
    ]
  }
}`),
		ExcludePlatforms: []string{"qemu"}, // etcd-member requires networking
		Distros:          []string{"cl"},
	})

	register.Register(&register.Test{
		Run:         etcdMemberV2BackupRestore,
		ClusterSize: 1,
		Name:        "cl.etcd-member.v2-backup-restore",
		UserData: conf.ContainerLinuxConfig(`

etcd:
  listen_client_urls:          http://0.0.0.0:4001,http://{PRIVATE_IPV4}:2379
  advertise_client_urls:       http://{PRIVATE_IPV4}:2379
  listen_peer_urls:            http://0.0.0.0:2380
  initial_advertise_peer_urls: http://{PRIVATE_IPV4}:2380
  discovery:                   $discovery
`),
		ExcludePlatforms: []string{"qemu", "esx"}, // etcd-member requires networking and ct rendering
		Distros:          []string{"cl"},
	})

	register.Register(&register.Test{
		Run: etcdmemberEtcdctlV3,
		// Clustersize of 1 to avoid needing private ips everywhere for clustering;
		// this lets it run on more platforms, and also faster
		ClusterSize: 1,
		Name:        "cl.etcd-member.etcdctlv3",
		UserData: conf.ContainerLinuxConfig(`

etcd:
  listen_client_urls:          http://0.0.0.0:2379
  advertise_client_urls:       http://127.0.0.1:2379
  listen_peer_urls:            http://0.0.0.0:2380
  initial_advertise_peer_urls: http://127.0.0.1:2380
`),
		ExcludePlatforms: []string{"qemu"}, // networking to download etcd image
		Distros:          []string{"cl"},
	})
}

func Discovery(c cluster.TestCluster) {
	var err error

	// NOTE(pb): this check makes the next code somewhat redundant
	if err = GetClusterHealth(c, c.Machines()[0], len(c.Machines())); err != nil {
		c.Fatalf("discovery failed cluster-health check: %v", err)
	}

	var keyMap map[string]string
	keyMap, err = setKeys(c, 5)
	if err != nil {
		c.Fatalf("failed to set keys: %v", err)
	}

	if err = checkKeys(c, keyMap); err != nil {
		c.Fatalf("failed to check keys: %v", err)
	}

}

// etcdMemberV2BackupRestore tests that the basic etcdctl v2 operations (get,
// put, rm) work. It verifies that a backup and restore, similar to the one
// documented in
// https://coreos.com/etcd/docs/latest/v2/admin_guide.html#disaster-recovery
// works.
// Note, this is a v2 backup/restore being performed against the current v3
// etcd
func etcdMemberV2BackupRestore(c cluster.TestCluster) {
	m := c.Machines()[0]

	if err := GetClusterHealth(c, c.Machines()[0], len(c.Machines())); err != nil {
		c.Fatalf("failed cluster-health check: %v", err)
	}

	c.MustSSH(m, `
	set -e

	prefix=$RANDOM
	etcdctl set /$prefix/test magic
	res="$(etcdctl get /$prefix/test)"
	if [[ "$res" != "magic" ]]; then
		echo "Expected magic, got $res"
		exit 1
	fi

	backup_to="$(mktemp -d)"

	sudo etcdctl backup --data-dir=/var/lib/etcd \
	               --backup-dir "${backup_to}"
	
	etcdctl rm /$prefix/test

	if etcdctl get /$prefix/test 2>&1; then
		echo "Expected rm'd key to error on get, didn't"
		exit 1
	fi

	sudo systemctl stop etcd-member

	# Note: this means we're now a new cluster of size 1 because of how etcd2
	# backup/restore works.
	sudo rm -rf /var/lib/etcd
	sudo mv "${backup_to}" /var/lib/etcd/
	sudo chown -R etcd:etcd /var/lib/etcd

	sudo mkdir -p /run/systemd/system/etcd-member.service.d/
	sudo tee /run/systemd/system/etcd-member.service.d/10-force-new.conf <<EOF
[Service]
Environment=ETCD_FORCE_NEW_CLUSTER=true
EOF

	sudo systemctl daemon-reload
	sudo systemctl start etcd-member

	res="$(etcdctl get /$prefix/test)"
	if [[ "$res" != "magic" ]]; then
		echo "Expected magic after backup-restore, got $res"
		exit 1
	fi
`)
}

// etcdmemberEtcdctlV3 tests the basic operatoin of the ETCDCTL_API=3 behavior
// of the etcdctl we ship.
func etcdmemberEtcdctlV3(c cluster.TestCluster) {
	m := c.Machines()[0]

	type etcdMemberOutput struct {
		Members []struct {
			ID         uint64
			Name       string
			PeerURLs   []string
			ClientURLs []string
		}
	}
	memberJson := c.MustSSH(m, `ETCDCTL_API=3 etcdctl member list --write-out=json`)

	members := etcdMemberOutput{}
	if err := json.Unmarshal(memberJson, &members); err != nil {
		c.Fatalf("could not unmarshal %s: %s", memberJson, err)
	}

	if len(members.Members) != len(c.Machines()) {
		c.Fatalf("expected %v members; only got %v", len(c.Machines()), len(members.Members))
	}

	c.MustSSH(m, `
	set -e
	export ETCDCTL_API=3
	if [[ "$(etcdctl put foo bar)" != "OK" ]]; then
		echo "Put failed"
		exit 1
	fi

	value="$(etcdctl get foo -w json | jq '.kvs[].value' -r | base64 -d)"

	if [[ "${value}" != "bar" ]]; then
		echo "Reading our put failed: expected bar, got ${value}"
		exit 1
	fi

	backup_to="$(mktemp -d)"

	sudo -E etcdctl snapshot save "${backup_to}/snapshot.db"
	sudo -E etcdctl snapshot status "${backup_to}/snapshot.db"
`)
}
