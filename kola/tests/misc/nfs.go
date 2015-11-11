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

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/misc")

	nfsserverconf = config.CloudConfig{
		CoreOS: config.CoreOS{
			Units: []config.Unit{
				config.Unit{
					Name:    "rpc-statd.service",
					Command: "start",
				},
				config.Unit{
					Name:    "rpc-mountd.service",
					Command: "start",
				},
				config.Unit{
					Name:    "nfsd.service",
					Command: "start",
				},
			},
		},
		WriteFiles: []config.File{
			config.File{
				Content: "/tmp	*(ro,insecure,all_squash,no_subtree_check,fsid=0)",
				Path: "/etc/exports",
			},
		},
		Hostname: "nfs1",
	}

	mounttmpl = `[Unit]
Description=NFS Client
After=network-online.target
Requires=network-online.target
After=rpc-statd.service
Requires=rpc-statd.service

[Mount]
What=%s:/tmp
Where=/mnt
Type=nfs
Options=defaults,noexec,nfsvers=%d
`
)

func init() {
	register.Register(&register.Test{
		Run:         NFSv3,
		ClusterSize: 0,
		Name:        "linux.nfs.v3",
		Platforms:   []string{"qemu", "aws"},
	})
	register.Register(&register.Test{
		Run:         NFSv4,
		ClusterSize: 0,
		Name:        "linux.nfs.v4",
		Platforms:   []string{"qemu", "aws"},
	})
}

func testNFS(c platform.TestCluster, nfsversion int) error {
	m1, err := c.NewMachine(nfsserverconf.String())
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine: %s", err)
	}

	defer m1.Destroy()

	plog.Info("NFS server booted.")

	/* poke a file in /tmp */
	tmp, err := m1.SSH("mktemp")
	if err != nil {
		return fmt.Errorf("Machine.SSH: %s", err)
	}

	plog.Infof("Test file %q created on server.", tmp)

	c2 := config.CloudConfig{
		CoreOS: config.CoreOS{
			Units: []config.Unit{
				config.Unit{
					Name:    "mnt.mount",
					Command: "start",
					Content: fmt.Sprintf(mounttmpl, m1.PrivateIP(), nfsversion),
				},
			},
		},
		Hostname: "nfs2",
	}

	m2, err := c.NewMachine(c2.String())
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine: %s", err)
	}

	defer m2.Destroy()

	plog.Info("NFS client booted.")

	plog.Info("Waiting for NFS mount on client...")

	checkmount := func() error {
		status, err := m2.SSH("systemctl is-active mnt.mount")
		if err != nil || string(status) != "active" {
			return fmt.Errorf("mnt.mount status is %q: %v", status, err)
		}

		plog.Info("Got NFS mount.")
		return nil
	}

	if err = util.Retry(10, 3*time.Second, checkmount); err != nil {
		return err
	}

	_, err = m2.SSH(fmt.Sprintf("stat /mnt/%s", path.Base(string(tmp))))
	if err != nil {
		return fmt.Errorf("file %q does not exist", tmp)
	}

	return nil
}

// Test that the kernel NFS server and client work within CoreOS.
func NFSv3(c platform.TestCluster) error {
	return testNFS(c, 3)
}

// Test that NFSv4 without security works on CoreOS.
func NFSv4(c platform.TestCluster) error {
	return testNFS(c, 4)
}
