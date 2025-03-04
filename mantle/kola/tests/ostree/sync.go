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

package ostree

import (
	"fmt"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
)

// https://github.com/coreos/coreos-assembler/pull/3998#issuecomment-2589994641
var nfs_server_butane = conf.Butane(`variant: fcos
version: 1.5.0
storage:
  directories:
  - path: /var/nfs/share1
    mode: 0777
  - path: /var/nfs/share2
    mode: 0777
  - path: /var/nfs/share3
    mode: 0777
  - path: /var/nfs/share4
    mode: 0777
  files:
    - path: "/etc/exports"
      overwrite: true
      contents:
        inline: |
          /var/nfs  *(rw,no_root_squash,insecure,fsid=0)
          /var/nfs/share1  *(rw,no_root_squash,insecure)
          /var/nfs/share2  *(rw,no_root_squash,insecure)
          /var/nfs/share3  *(rw,no_root_squash,insecure)
          /var/nfs/share4  *(rw,no_root_squash,insecure)
    - path: "/var/lib/nfs/etab"
      user:
        name: nfsnobody
      group:
        name: nfsnobody
    - path: /usr/local/bin/block-nfs.sh
      mode: 0755
      overwrite: true
      contents:
        inline: |
          #!/bin/bash
          nft add table inet nfs
          nft add chain inet nfs INPUT { type filter hook input priority filter \; policy accept \; }
          nft add rule inet nfs INPUT tcp dport 2049 drop
systemd:
  units:
    - name: "nfs-server.service"
      enabled: true`)

func init() {
	register.RegisterTest(&register.Test{
		// See https://github.com/ostreedev/ostree/pull/2968
		Run:         ostreeSyncTest,
		ClusterSize: 0,
		Name:        "ostree.sync",
		Description: "Verify ostree can sync the filesystem with disconnected the NFS volume.",
		Distros:     []string{"rhcos"},
		Tags:        []string{"ostree", kola.SkipBaseChecksTag, kola.NeedsInternetTag},
	})
}

// NFS server
type NfsServer struct {
	Machine        platform.Machine
	MachineAddress string
}

func setupNFSMachine(c cluster.TestCluster) NfsServer {
	var m platform.Machine
	var err error
	var nfs_server string

	options := platform.QemuMachineOptions{
		HostForwardPorts: []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
			{Service: "nfs", HostPort: 2049, GuestPort: 2049},
		},
	}
	options.MinMemory = 2048
	// start the machine
	switch c := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *qemu.Cluster:
		m, err = c.NewMachineWithQemuOptions(nfs_server_butane, options)
		nfs_server = "10.0.2.2"
	default:
		m, err = c.NewMachine(nfs_server_butane)
		nfs_server = m.PrivateIP()
	}
	if err != nil {
		c.Fatal(err)
	}

	// Wait for nfs server to become active
	err = util.Retry(6, 10*time.Second, func() error {
		nfs_status, err := c.SSH(m, "systemctl is-active nfs-server.service")
		if err != nil {
			return err
		} else if string(nfs_status) != "active" {
			return fmt.Errorf("nfs-server.service is not ready: %s.", string(nfs_status))
		}
		return nil
	})
	if err != nil {
		c.Fatalf("Timeout(1m) while waiting for nfs-server.service to be ready: %v", err)
	}
	return NfsServer{
		Machine:        m,
		MachineAddress: nfs_server,
	}
}

// Refer to the steps:
// https://issues.redhat.com/browse/ECOENGCL-91?focusedId=26272587&page=com.atlassian.jira.plugin.system.issuetabpanels:comment-tabpanel#comment-26272587
func ostreeSyncTest(c cluster.TestCluster) {
	// Start nfs server machine
	nfs_server := setupNFSMachine(c)

	// Start test machine
	butane := conf.Butane(`variant: fcos
version: 1.5.0
storage:
  directories:
  - path: /var/tmp/data1
    mode: 0777
  - path: /var/tmp/data2
    mode: 0777
  - path: /var/tmp/data3
    mode: 0777
  - path: /var/tmp/data4
    mode: 0777
  files:
    - path: /etc/systemd/system.conf
      overwrite: true
      contents:
        inline: |
          [Manager]
          DefaultTimeoutStopSec=10s
    - path: /usr/local/bin/nfs-random-write.sh
      mode: 0755
      overwrite: true
      contents:
        inline: |
          #!/bin/bash
          i=$1
          while true; do
            sudo dd if=/dev/urandom of=/var/tmp/data$i/test bs=4096 count=2048 conv=notrunc oflag=append &> /dev/null
            sleep 0.1
            sudo rm -f /var/tmp/data$i/test
          done`)
	opts := platform.MachineOptions{
		MinMemory: 2048,
	}
	var nfs_client platform.Machine
	var err error

	switch c := c.Cluster.(type) {
	case *qemu.Cluster:
		nfs_client, err = c.NewMachineWithOptions(butane, opts)
	default:
		nfs_client, err = c.NewMachine(butane)
	}
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	// Wait for test machine
	err = util.Retry(6, 10*time.Second, func() error {
		// entry point /var/nfs with fsid=0 will be root for clients
		// refer to https://access.redhat.com/solutions/107793
		_ = c.MustSSHf(nfs_client, `for i in $(seq 4); do
			sudo mount -t nfs4 %s:/share$i /var/tmp/data$i
			done`, nfs_server.MachineAddress)

		mounts := c.MustSSH(nfs_client, "sudo df -Th | grep nfs | wc -l")
		if string(mounts) != "4" {
			c.Fatalf("Can not mount all nfs")
		}
		c.Log("Got NFS mount.")
		return nil
	})
	if err != nil {
		c.Fatalf("Timeout(1m) to get nfs mount: %v", err)
	}

	doSyncTest(c, nfs_client, nfs_server.Machine)
}

func doSyncTest(c cluster.TestCluster, client platform.Machine, m platform.Machine) {
	// Do simple touch to make sure nfs server works
	c.RunCmdSync(client, "sudo touch /var/tmp/data3/test")
	// Continue writing while doing test
	// gets run using systemd unit
	for i := 1; i <= 4; i++ {
		cmd := fmt.Sprintf("sudo systemd-run --unit=nfs%d --no-block sh -c '/usr/local/bin/nfs-random-write.sh %d'", i, i)
		_, err := c.SSH(client, cmd)
		if err != nil {
			c.Fatalf("failed to run nfs-random-write: %v", err)
		}
	}

	// block NFS traffic on nfs server
	c.RunCmdSync(m, "sudo /usr/local/bin/block-nfs.sh")
	// Create a stage deploy using kargs while writing
	c.RunCmdSync(client, "sudo rpm-ostree kargs --append=test=1")

	err := client.Reboot()
	if err != nil {
		c.Fatalf("Couldn't reboot machine: %v", err)
	}

	err = util.Retry(12, 10*time.Second, func() error {
		// Look for the kernel argument test=1
		kernelArguments, err := c.SSH(client, "cat /proc/cmdline")
		if err != nil {
			return err
		} else if !strings.Contains(string(kernelArguments), "test=1") {
			c.Fatalf("Not found test=1 in kernel argument after rebooted")
		}
		return nil
	})
	if err != nil {
		c.Fatalf("Unable to reboot machine: %v", err)
	}
	c.Log("Found test=1 in kernel argument after rebooted.")
}
