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
	"bytes"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/coreos/mantle/platform"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
)

// Test that the kernel NFS server and client work within CoreOS.
func NFS(c platform.Cluster) error {
	/* server machine */
	c1 := config.CloudConfig{
		CoreOS: config.CoreOS{
			Units: []config.Unit{
				config.Unit{
					Name:    "rpcbind.service",
					Command: "start",
				},
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

	m1, err := c.NewMachine(c1.String())
	if err != nil {
		return fmt.Errorf("Cluster.NewMachine: %s", err)
	}

	defer m1.Destroy()

	log.Printf("NFS server booted.")

	/* poke a file in /tmp */
	tmp, err := m1.SSH("mktemp")
	if err != nil {
		return fmt.Errorf("Machine.SSH: %s", err)
	}

	log.Printf("Test file %q created on server.", tmp)

	/* client machine */

	nfstmpl := `[Unit]
Description=NFS Client
After=network-online.target
Requires=network-online.target
After=rpc-statd.service
Requires=rpc-statd.service

[Mount]
What=%s:/tmp
Where=/mnt
Type=nfs
Options=defaults,noexec
`

	c2 := config.CloudConfig{
		CoreOS: config.CoreOS{
			Units: []config.Unit{
				config.Unit{
					Name:    "rpc-statd.service",
					Command: "start",
				},
				config.Unit{
					Name:    "mnt.mount",
					Command: "start",
					Content: fmt.Sprintf(nfstmpl, m1.IP()),
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

	log.Printf("NFS client booted.")

	var lsmnt []byte

	log.Printf("Waiting for NFS mount on client...")

	/* there's probably a better wait to check the mount */
	for i := 0; i < 5; i++ {
		lsmnt, _ = m2.SSH("ls /mnt")

		if len(lsmnt) != 0 {
			log.Printf("Got NFS mount.")
			break
		}

		time.Sleep(1 * time.Second)
	}

	if len(lsmnt) == 0 {
		return fmt.Errorf("Client /mnt is empty.")
	}

	if bytes.Contains(lsmnt, []byte(path.Base(string(tmp)))) != true {
		return fmt.Errorf("Client /mnt did not contain file %q from server /tmp -- /mnt: %s", tmp, lsmnt)
	}

	log.Printf("NFS test passed.")

	return nil
}
