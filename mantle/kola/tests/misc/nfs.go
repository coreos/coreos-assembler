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

package misc

import (
	"fmt"
	"path"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

var (
	// TODO: Needs to be update for Fedora CoreOS support
	nfsserverconf = conf.Butane(`
variant: fcos
version: 1.3.0
storage:
  files:
    - filesystem: "root"
      path: "/etc/hostname"
      contents:
        inline: "nfs1"
      mode: 0644
    - filesystem: "root"
      path: "/etc/exports"
      contents:
        inline: "/tmp  *(ro,insecure,all_squash,no_subtree_check,fsid=0)"
      mode: 0644
    - filesystem: "root"
      path: "/var/lib/nfs/etab"
      mode: 0644
systemd:
  units:
    - name: "nfs-server.service"
      enabled: true`)
)

func init() {
	// TODO: enable FCOS
	register.RegisterTest(&register.Test{
		Run:            NFSv4,
		ClusterSize:    0,
		Name:           "linux.nfs.v4",
		ExcludeDistros: []string{"fcos"},

		// Disabled on Azure because setting hostname
		// is required at the instance creation level
		// qemu-unpriv machines cannot communicate
		ExcludePlatforms: []string{"azure", "qemu-unpriv"},
	})
}

func testNFS(c cluster.TestCluster, nfsversion int, remotePath string) {
	m1, err := c.NewMachine(nfsserverconf)
	if err != nil {
		c.Fatalf("Cluster.NewMachine: %s", err)
	}

	defer m1.Destroy()

	c.Log("NFS server booted.")

	/* poke a file in /tmp */
	tmp := c.MustSSH(m1, "mktemp")

	c.Logf("Test file %q created on server.", tmp)

	nfstype := "nfs"
	if nfsversion == 4 {
		nfstype = "nfs4"
	}

	// TODO: Needs to be update for Fedora CoreOS support
	c2 := conf.Butane(fmt.Sprintf(`
variant: fcos
version: 1.3.0
storage:
  files:
    - filesystem: "root"
      path: "/etc/hostname"
      contents:
        inline: "nfs2"
      mode: 0644
systemd:
  units:
    - name: "var-mnt.mount"
      enabled: true
      contents: |-
        [Unit]
        Description=NFS Client
        After=network-online.target
        Requires=network-online.target
        After=rpc-statd.service
        Requires=rpc-statd.service

        [Mount]
        What=%s:%s
        Where=/var/mnt
        Type=%s
        Options=defaults,noexec,nfsvers=%d

        [Install]
        WantedBy=multi-user.target`, m1.PrivateIP(), remotePath, nfstype, nfsversion))

	m2, err := c.NewMachine(c2)
	if err != nil {
		c.Fatalf("Cluster.NewMachine: %s", err)
	}

	defer m2.Destroy()

	c.Log("NFS client booted.")

	checkmount := func() error {
		status, err := c.SSH(m2, "systemctl is-active var-mnt.mount")
		if err != nil || string(status) != "active" {
			return fmt.Errorf("var-mnt.mount status is %q: %v", status, err)
		}

		c.Log("Got NFS mount.")
		return nil
	}

	if err = util.Retry(10, 3*time.Second, checkmount); err != nil {
		c.Fatal(err)
	}

	c.RunCmdSyncf(m2, "stat /var/mnt/%s", path.Base(string(tmp)))
}

// Test that NFSv4 without security works.
func NFSv4(c cluster.TestCluster) {
	testNFS(c, 4, "/")
}
