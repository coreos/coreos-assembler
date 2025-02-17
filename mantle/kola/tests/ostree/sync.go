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
  - path: /var/nfs/share5
    mode: 0777
  - path: /var/nfs/share6
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
          /var/nfs/share5  *(rw,no_root_squash,insecure)
          /var/nfs/share6  *(rw,no_root_squash,insecure)
    - path: "/var/lib/nfs/etab"
      user:
        name: nfsnobody
      group:
        name: nfsnobody
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
  - path: /var/tmp/data5
    mode: 0777
  - path: /var/tmp/data6
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
          for i in $(seq 6); do
            (while sudo rm -f /var/tmp/data$i/test; do
              for x in $(seq 6); do
                sudo dd if=/dev/urandom of=/var/tmp/data$i/test bs=4096 count=2048 conv=notrunc oflag=append &> /dev/null;
                sleep 0.5;
              done;
            done) &
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
		_ = c.MustSSHf(nfs_client, `for i in $(seq 6); do
			sudo mount -t nfs4 %s:/share$i /var/tmp/data$i
			done`, nfs_server.MachineAddress)

		mounts := c.MustSSH(nfs_client, "sudo df -Th | grep nfs | wc -l")
		if string(mounts) != "6" {
			c.Fatalf("Can not mount all nfs")
		}
		c.Log("Got NFS mount.")
		return nil
	})
	if err != nil {
		c.Fatalf("Timeout(1m) to get nfs mount: %v", err)
	}

	doSyncTest(c, nfs_client)
}

func doSyncTest(c cluster.TestCluster, client platform.Machine) {
	c.RunCmdSync(client, "sudo touch /var/tmp/data3/test")
	// Continue writing while doing test
	go func() {
		_, err := c.SSH(client, "sudo sh /usr/local/bin/nfs-random-write.sh")
		if err != nil {
			c.Fatalf("failed to start write-to-nfs: %v", err)
		}
	}()

	// Create a stage deploy using kargs while writing
	c.RunCmdSync(client, "sudo rpm-ostree kargs --append=test=1")

	netdevices := c.MustSSH(client, "ls /sys/class/net | grep -v lo")
	netdevice := string(netdevices)
	if netdevice == "" {
		c.Fatalf("failed to get net device")
	}
	c.Log("Set link down and rebooting.")
	// Skip the error check as it is expected
	cmd := fmt.Sprintf("sudo systemd-run sh -c 'ip link set %s down && sleep 2 && systemctl reboot'", netdevice)
	_, _ = c.SSH(client, cmd)

	time.Sleep(5 * time.Second)
	err := util.Retry(8, 10*time.Second, func() error {
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
