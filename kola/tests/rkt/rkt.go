// Copyright 2016 CoreOS, Inc.
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

package rkt

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/qemu"
	"github.com/coreos/mantle/util"
)

var config = conf.Ignition(`{
	"ignition": {
		"version": "2.0.0"
	},
	"systemd": {
		"units": [{
			"name": "etcd-member.service",
			"enable": true
		}]
	}
}`)

func init() {
	register.Register(&register.Test{
		Run:              rktEtcd,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu"},
		Name:             "coreos.rkt.etcd3",
		UserData:         config,
	})

	register.Register(&register.Test{
		Name:        "rkt.base",
		ClusterSize: 1,
		Run:         rktBase,
	})

}

func rktEtcd(c cluster.TestCluster) {
	m := c.Machines()[0]

	etcdCmd := "etcdctl cluster-health"
	etcdCheck := func() error {
		output, err := m.SSH(etcdCmd)
		if err != nil {
			return fmt.Errorf("failed to run %q: output: %q status: %q", etcdCmd, output, err)
		}

		return nil
	}

	if err := util.Retry(60, 3*time.Second, etcdCheck); err != nil {
		c.Fatalf("etcd in rkt failed health check: %v", err)
	}
}

// we use subtests to improve testing performance here. Creating the aci is
// more expensive than actually running most of these tests.
func rktBase(c cluster.TestCluster) {
	m := c.Machines()[0]

	// TODO this should not be necessary, but is at the time of writing
	m.SSH("sudo setenforce 0")

	createTestAci(c, m, "test.rkt.aci", []string{"echo", "sleep", "sh"})

	journalForPodContains := func(c cluster.TestCluster, uuidFile string, contains string) {
		output, err := m.SSH(fmt.Sprintf("journalctl --dir /var/log/journal/$(cat %s | sed 's/-//g')", uuidFile))
		if err != nil {
			c.Fatalf("error running journalctl: %v", err)
		}
		if !bytes.Contains(output, []byte(contains)) {
			c.Fatalf("expected journal logs from machine dir to include app output %q; was %s", contains, output)
		}
	}

	c.Run("cli", func(c cluster.TestCluster) {
		uuidFile := "/tmp/run-test.uuid"

		output, err := m.SSH(fmt.Sprintf("sudo rkt run --uuid-file-save=%s test.rkt.aci:latest --exec=sh -- -c 'echo success'", uuidFile))
		if err != nil {
			c.Fatalf("failed to run test aci: %v, %s", err, output)
		}
		defer m.SSH(fmt.Sprintf("sudo rkt rm --uuid-file=%s", uuidFile))

		if !bytes.Contains(output, []byte("success")) {
			c.Fatalf("expected rkt stdout to include app output ('success'); was %s", output)
		}

		journalForPodContains(c, uuidFile, "success")
	})

	c.Run("unit", func(c cluster.TestCluster) {
		uuidFile := "/tmp/run-as-unit-test.uuid"

		output, err := m.SSH(fmt.Sprintf("sudo systemd-run --quiet --unit run-as-unit.service -- rkt run --uuid-file-save=%s test.rkt.aci:latest --exec=sh -- -c 'echo success'", uuidFile))
		if err != nil {
			c.Fatalf("failed to systemd-run rkt: %v, %s", err, output)
		}
		defer m.SSH(fmt.Sprintf("sudo rkt rm --uuid-file=%s", uuidFile))

		output, err = m.SSH(fmt.Sprintf("while ! [ -s %s ]; do sleep 0.1; done; rkt status --wait $(cat %s)", uuidFile, uuidFile))
		if err != nil {
			c.Fatalf("error waiting for rkt: %v, %s", err, output)
		}

		journalForPodContains(c, uuidFile, "success")
	})

	c.Run("machinectl-integration", func(c cluster.TestCluster) {
		uuidFile := "/tmp/run-machinectl.uuid"

		output, err := m.SSH(fmt.Sprintf("sudo systemd-run --quiet --unit run-machinectl -- rkt run --uuid-file-save=%s test.rkt.aci:latest --exec=sleep -- inf", uuidFile))
		if err != nil {
			c.Fatalf("failed to run test aci: %v, %s", err, output)
		}
		defer m.SSH(fmt.Sprintf("sudo rkt rm --uuid-file=%s", uuidFile))

		output, err = m.SSH(fmt.Sprintf("while ! [ -s %s ]; do sleep 0.1; done; rkt status --wait-ready $(cat %s)", uuidFile, uuidFile))
		if err != nil {
			c.Fatalf("error waiting for rkt: %v, %s", err, output)
		}

		machinectlOutput, err := m.SSH(fmt.Sprintf("machinectl show rkt-$(cat %s)", uuidFile))
		if err != nil {
			c.Fatalf("error running machinectl: %v, %s", err, output)
		}

		for _, line := range []string{"State=running", "Class=container", "Service=rkt"} {
			if !bytes.Contains(machinectlOutput, []byte(line)) {
				c.Fatalf("expected machinectl to include %q: was %s", line, machinectlOutput)
			}
		}

		output, err = m.SSH(fmt.Sprintf("sudo rkt stop --uuid-file=%s", uuidFile))
		if err != nil {
			c.Fatalf("error stopping app: %v, %s", err, output)
		}
		output, err = m.SSH(fmt.Sprintf("rkt status --wait $(cat %s)", uuidFile))
		if err != nil {
			c.Fatalf("error waiting for app to stop: %v, %s", err, output)
		}
	})
}

// TODO: once rkt can fetch a local 'docker' image, using `genDockerContainer`
// from the docker test file could be a better solution.
func createTestAci(c cluster.TestCluster, m platform.Machine, name string, bins []string) {
	// Has format strings for:
	// 1) aci name
	// 2) arch
	testAciManifest := `{
	"acKind": "ImageManifest",
	"acVersion": "0.8.9",
	"name": "%s",
	"labels": [{"name": "os","value": "linux"},{"name": "arch","value": "%s"},{"name": "version","value": "latest"}]
}`

	arch := "amd64"
	if _, ok := c.Cluster.(*qemu.Cluster); ok && kola.QEMUOptions.Board == "arm64-usr" {
		arch = "aarch64"
	}

	output, err := m.SSH(`set -e
	tmpdir=$(mktemp -d)
	cd $tmpdir
	cat > manifest <<EOF
` + fmt.Sprintf(testAciManifest, name, arch) + `
EOF

	mkdir rootfs
	bins=$(which ` + strings.Join(bins, " ") + `)
	libs=$(sudo ldd $bins | grep -o /lib'[^ ]*' | sort -u)
	sudo rsync -av --relative --copy-links $bins $libs ./rootfs/

	sudo tar cf /tmp/test-aci.aci .
	sudo rkt image fetch --insecure-options=image /tmp/test-aci.aci
	cd
	sudo rm -rf /tmp/test-aci.aci $tmpdir`)

	if err != nil {
		c.Fatalf("failed to create aci %s: %v, %s", name, err, output)
	}
}
