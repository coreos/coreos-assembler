// Copyright 2018 CoreOS, Inc.
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

package update

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-omaha/omaha"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	tutil "github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/local"
	"github.com/coreos/mantle/util"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.update.payload",
		Run:         payload,
		ClusterSize: 1,
		NativeFuncs: map[string]func() error{
			"Omaha": Serve,
		},
	})
}

func Serve() error {
	omahaserver, err := omaha.NewTrivialServer(":34567")
	if err != nil {
		return fmt.Errorf("creating trivial omaha server: %v\n", err)
	}

	omahawrapper := local.OmahaWrapper{TrivialServer: omahaserver}

	if err = omahawrapper.AddPackage("/updates/update.gz", "update.gz"); err != nil {
		return fmt.Errorf("bad payload: %v", err)
	}

	return omahawrapper.Serve()
}

func payload(c cluster.TestCluster) {
	addr := configureOmahaServer(c, c.Machines()[0])

	// create the actual test machine, the machine
	// that is created by the test registration is
	// used to host the omaha server
	m, err := c.NewMachine(nil)
	if err != nil {
		c.Fatalf("creating test machine: %v", err)
	}

	// Machines are intentionally configured post-boot
	// via SSH to allow for testing versions which predate
	// Ignition
	configureMachineForUpdate(c, m, addr)

	tutil.AssertBootedUsr(c, m, "USR-A")

	updateMachine(c, m)

	tutil.AssertBootedUsr(c, m, "USR-B")

	tutil.InvalidateUsrPartition(c, m, "USR-A")

	updateMachine(c, m)

	tutil.AssertBootedUsr(c, m, "USR-A")
}

func configureOmahaServer(c cluster.TestCluster, srv platform.Machine) string {
	if kola.UpdatePayloadFile == "" {
		c.Skip("no update payload provided")
	}

	in, err := os.Open(kola.UpdatePayloadFile)
	if err != nil {
		c.Fatalf("opening update payload: %v", err)
	}
	defer in.Close()
	if err := platform.InstallFile(in, srv, "/updates/update.gz"); err != nil {
		c.Fatalf("copying update payload to omaha server: %v", err)
	}

	c.MustSSH(srv, fmt.Sprintf("sudo systemd-run --quiet ./kolet run %s Omaha", c.Name()))

	err = util.WaitUntilReady(60*time.Second, 5*time.Second, func() (bool, error) {
		_, _, err := srv.SSH(fmt.Sprintf("curl %s:34567", srv.PrivateIP()))
		return err == nil, nil
	})
	if err != nil {
		c.Fatal("timed out waiting for omaha server to become active")
	}

	return fmt.Sprintf("%s:34567", srv.PrivateIP())
}

func configureMachineForUpdate(c cluster.TestCluster, m platform.Machine, addr string) {
	// update atomicly so nothing reading update.conf fails
	c.MustSSH(m, fmt.Sprintf(`sudo bash -c "cat >/etc/coreos/update.conf.new <<EOF
GROUP=developer
SERVER=http://%s/v1/update
EOF"`, addr))
	c.MustSSH(m, "sudo mv /etc/coreos/update.conf{.new,}")

	// inject dev key
	c.MustSSH(m, `sudo bash -c "cat >/etc/coreos/update-payload-key.pub.pem <<EOF
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAzFS5uVJ+pgibcFLD3kbY
k02Edj0HXq31ZT/Bva1sLp3Ysv+QTv/ezjf0gGFfASdgpz6G+zTipS9AIrQr0yFR
+tdp1ZsHLGxVwvUoXFftdapqlyj8uQcWjjbN7qJsZu0Ett/qo93hQ5nHW7Sv5dRm
/ZsDFqk2Uvyaoef4bF9r03wYpZq7K3oALZ2smETv+A5600mj1Xg5M52QFU67UHls
EFkZphrGjiqiCdp9AAbAvE7a5rFcJf86YR73QX08K8BX7OMzkn3DsqdnWvLB3l3W
6kvIuP+75SrMNeYAcU8PI1+bzLcAG3VN3jA78zeKALgynUNH50mxuiiU3DO4DZ+p
5QIDAQAB
-----END PUBLIC KEY-----
EOF"`)

	c.MustSSH(m, "sudo mount --bind /etc/coreos/update-payload-key.pub.pem /usr/share/update_engine/update-payload-key.pub.pem")

	// disable reboot so the test has explicit control
	c.MustSSH(m, "sudo systemctl mask --now locksmithd.service")
	c.MustSSH(m, "sudo systemctl reset-failed locksmithd.service")

	c.MustSSH(m, "sudo systemctl restart update-engine.service")
}

func updateMachine(c cluster.TestCluster, m platform.Machine) {
	c.Logf("Triggering update_engine")

	out, stderr, err := m.SSH("update_engine_client -check_for_update")
	if err != nil {
		c.Fatalf("Executing update_engine_client failed: %v: %v: %s", out, err, stderr)
	}

	err = util.WaitUntilReady(120*time.Second, 10*time.Second, func() (bool, error) {
		envs, stderr, err := m.SSH("update_engine_client -status 2>/dev/null")
		if err != nil {
			return false, fmt.Errorf("checking status failed: %v: %s", err, stderr)
		}

		return splitNewlineEnv(string(envs))["CURRENT_OP"] == "UPDATE_STATUS_UPDATED_NEED_REBOOT", nil
	})
	if err != nil {
		c.Fatalf("waiting for UPDATE_STATUS_UPDATED_NEED_REBOOT: %v", err)
	}

	c.Logf("Rebooting test machine")

	if err = m.Reboot(); err != nil {
		c.Fatalf("reboot failed: %v", err)
	}
}

// splits newline-delimited KEY=VAL pairs into a map
func splitNewlineEnv(envs string) map[string]string {
	m := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(envs))
	for sc.Scan() {
		spl := strings.SplitN(sc.Text(), "=", 2)
		m[spl[0]] = spl[1]
	}
	return m
}
