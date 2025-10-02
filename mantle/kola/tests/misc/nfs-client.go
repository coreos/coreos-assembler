// Copyright 2025 Red Hat, Inc.
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

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
)

const (
	machine_conf = `
variant: fcos
version: 1.5.0
storage:
  files:
    - path: /etc/containers/systemd/nfs.container
      overwrite: true
      contents:
        inline: |
          [Container]
          Image=quay.io/coreos-assembler/nfs
          Volume=/tmp:/export
          Network=host
          PodmanArgs=--privileged
          [Install]
          WantedBy=default.target
    - path: "/etc/hostname"
      contents:
        inline: "nfs-client"
      mode: 0644
systemd:
  units:
    - name: "var-mnt-nfsv4.mount"
      enabled: true
      contents: |-
        [Unit]
        Description=NFS Client
        After=network-online.target
        Requires=network-online.target
        After=rpc-statd.service nfs.service
        Requires=rpc-statd.service

        [Mount]
        What=127.0.0.1:/
        Where=/var/mnt/nfsv4
        Type=nfs4
        Options=defaults,noexec,nfsvers=4

        [Install]
        WantedBy=multi-user.target
    - name: "var-mnt-nfs.mount"
      enabled: true
      contents: |-
        [Unit]
        Description=NFS Client
        After=network-online.target
        Requires=network-online.target
        After=rpc-statd.service nfs.service
        Requires=rpc-statd.service

        [Mount]
        What=127.0.0.1:/export
        Where=/var/mnt/nfs
        Type=nfs
        Options=vers=3

        [Install]
        WantedBy=multi-user.target`
)

// Test nfs client

func init() {
	register.RegisterTest(&register.Test{
		Run:         nfsClientTest,
		ClusterSize: 1,
		UserData:    conf.Butane(machine_conf),
		Name:        "linux.nfs.client",
		Description: "Verifies NFS client works.",
		Tags:        []string{kola.NeedsInternetTag},
		Platforms:   []string{"qemu"},

		// RHCOS has a separate test for NFS v4 server and client
		ExcludeDistros: []string{"rhcos", "scos"},
	})
}

func nfsClientTest(c cluster.TestCluster) {

	nfs_machine := c.Machines()[0]

	// Wait for nfs server to become active
	// 1 minutes should be enough to pull the container image
	err := util.Retry(4, 15*time.Second, func() error {

		nfs_status, err := c.SSH(nfs_machine, "systemctl is-active nfs.service")

		if err != nil {
			return err
		} else if string(nfs_status) == "inactive" {
			return fmt.Errorf("nfs.service is not ready: %s.", string(nfs_status))
		}
		return nil
	})
	if err != nil {
		c.Fatalf("Timed out while waiting for nfs.service to be ready: %v", err)
	}

	c.Log("NFS server booted")

	// poke a file in /tmp
	tmp := c.MustSSH(nfs_machine, "mktemp")

	checkv4mount := func() error {
		status, err := c.SSH(nfs_machine, "systemctl is-active var-mnt-nfsv4.mount")
		if err != nil || string(status) != "active" {
			return fmt.Errorf("var-mnt-nfsv4.mount status is %q: %v", status, err)
		}

		c.Log("Got NFSv4 mount.")
		return nil
	}

	if err = util.Retry(10, 3*time.Second, checkv4mount); err != nil {
		c.Fatal(err)
	}

	checkmount := func() error {
		status, err := c.SSH(nfs_machine, "systemctl is-active var-mnt-nfs.mount")
		if err != nil || string(status) != "active" {
			return fmt.Errorf("var-mnt-nfs.mount status is %q: %v", status, err)
		}

		c.Log("Got NFSv3 mount.")
		return nil
	}

	if err = util.Retry(10, 3*time.Second, checkmount); err != nil {
		c.Fatal(err)
	}

	c.RunCmdSyncf(nfs_machine, "stat /var/mnt/nfsv4/%s", path.Base(string(tmp)))
	c.RunCmdSyncf(nfs_machine, "stat /var/mnt/nfs/%s", path.Base(string(tmp)))
}
