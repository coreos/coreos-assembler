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

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/agent"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/machine/qemu"
	"github.com/coreos/mantle/sdk"
	sdkomaha "github.com/coreos/mantle/sdk/omaha"
)

var (
	updateTimeout    time.Duration
	updatePayload    string
	cmdUpdatePayload = &cobra.Command{
		Run:    runUpdatePayload,
		PreRun: preRun,
		Use:    "updatepayload",
		Short:  "test serving a update_engine payload",
		Long: `
Boot a CoreOS instance and serve an update payload to its update_engine.

This command must run inside of the SDK as root, e.g.

sudo kola updatepayload
`,
	}
)

type userdataParams struct {
	Port int
	Keys []*agent.Key
}

// The user data is a bash script executed by cloudinit to ensure
// compatibility with all versions of CoreOS.
const userdataTmpl = `#!/bin/bash -ex

# add ssh key on exit to avoid racing w/ test harness
do_ssh_keys() {
	update-ssh-keys -u core -a updatepayload <<-EOF
		{{range .Keys}}{{.}}
		{{end}}
	EOF
}
trap do_ssh_keys EXIT

# update atomicly so nothing reading update.conf fails
cat >/etc/coreos/update.conf.new <<EOF
GROUP=developer
SERVER=http://10.0.0.1:{{printf "%d" .Port}}/v1/update/
EOF
mv /etc/coreos/update.conf{.new,}

# inject the dev key so official images can be used for testing
cat >/etc/coreos/update-payload-key.pub.pem <<EOF
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAzFS5uVJ+pgibcFLD3kbY
k02Edj0HXq31ZT/Bva1sLp3Ysv+QTv/ezjf0gGFfASdgpz6G+zTipS9AIrQr0yFR
+tdp1ZsHLGxVwvUoXFftdapqlyj8uQcWjjbN7qJsZu0Ett/qo93hQ5nHW7Sv5dRm
/ZsDFqk2Uvyaoef4bF9r03wYpZq7K3oALZ2smETv+A5600mj1Xg5M52QFU67UHls
EFkZphrGjiqiCdp9AAbAvE7a5rFcJf86YR73QX08K8BX7OMzkn3DsqdnWvLB3l3W
6kvIuP+75SrMNeYAcU8PI1+bzLcAG3VN3jA78zeKALgynUNH50mxuiiU3DO4DZ+p
5QIDAQAB
-----END PUBLIC KEY-----
EOF
mount --bind /etc/coreos/update-payload-key.pub.pem \
	/usr/share/update_engine/update-payload-key.pub.pem

# disable reboot so we have explicit control
systemctl mask locksmithd.service
systemctl stop locksmithd.service
systemctl reset-failed locksmithd.service

# off we go!
systemctl restart update-engine.service
`

func init() {
	cmdUpdatePayload.Flags().DurationVar(
		&updateTimeout, "timeout", 120*time.Second,
		"maximum time to wait for update")
	cmdUpdatePayload.Flags().StringVar(
		&updatePayload, "payload", "",
		"update payload")
	root.AddCommand(cmdUpdatePayload)
}

func runUpdatePayload(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		plog.Fatal("No args accepted")
	}

	if updatePayload == "" {
		updatePayload = newPayload()
	}

	start := time.Now()
	plog.Notice("=== Running CoreOS upgrade test")
	if err := runUpdateTest(); err != nil {
		plog.Fatalf("--- FAIL: %v (%s)", err, time.Since(start))
	}
	plog.Noticef("--- PASS: CoreOS upgrade test (%s)", time.Since(start))
}

func runUpdateTest() error {
	cluster, err := qemu.NewCluster(&kola.QEMUOptions)
	if err != nil {
		return fmt.Errorf("new cluster: %v", err)
	}
	defer cluster.Destroy()
	qc := cluster.(*qemu.Cluster)

	if err := qc.OmahaServer.SetPackage(updatePayload); err != nil {
		return fmt.Errorf("bad payload: %v", err)
	}

	cfg, err := newUserdata(qc)
	if err != nil {
		return fmt.Errorf("bad userdata: %v", err)
	}

	plog.Infof("Spawning test machine")

	m, err := cluster.NewMachine(cfg)
	if err != nil {
		return fmt.Errorf("new machine: %v", err)
	}

	// initial boot
	if err := startJournal(m); err != nil {
		return fmt.Errorf("initial boot: %v", err)
	}

	if err := checkUsrA(m); err != nil {
		return fmt.Errorf("initial boot: %v", err)
	}

	if err := tryUpdate(m); err != nil {
		return fmt.Errorf("first update: %v", err)
	}

	// second boot
	if err := startJournal(m); err != nil {
		return fmt.Errorf("second boot: %v", err)
	}

	if err := checkUsrB(m); err != nil {
		return fmt.Errorf("second boot: %v", err)
	}

	// Invalidate USR-A to ensure the update is legit.
	if out, err := m.SSH("sudo coreos-setgoodroot && " +
		"sudo wipefs /dev/disk/by-partlabel/USR-A"); err != nil {
		return fmt.Errorf("invalidating USR-A failed: %v: %v", out, err)
	}

	if err := tryUpdate(m); err != nil {
		return fmt.Errorf("second update: %v", err)
	}

	// third boot
	if err := startJournal(m); err != nil {
		return fmt.Errorf("third boot: %v", err)
	}

	if err := checkUsrA(m); err != nil {
		return fmt.Errorf("third boot: %v", err)
	}

	return nil
}

func tryUpdate(m platform.Machine) error {
	plog.Infof("Triggering update_engine")

	/* trigger update, monitor the progress. */
	out, err := m.SSH("update_engine_client -check_for_update")
	if err != nil {
		return fmt.Errorf("Executing update_engine_client failed: %v: %v", out, err)
	}

	start := time.Now()
	status := "unknown"
	for status != "UPDATE_STATUS_UPDATED_NEED_REBOOT" && time.Since(start) < updateTimeout {
		time.Sleep(10 * time.Second)

		envs, err := m.SSH("update_engine_client -status 2>/dev/null")
		if err != nil {
			return fmt.Errorf("checking status failed: %v", err)
		}

		em := splitNewlineEnv(string(envs))
		status = em["CURRENT_OP"]
	}

	if status != "UPDATE_STATUS_UPDATED_NEED_REBOOT" {
		return fmt.Errorf("failed to complete within %s, current status %s", updateTimeout, status)
	}

	plog.Info("Rebooting test machine")

	/* reboot it */
	if err := platform.Reboot(m); err != nil {
		return fmt.Errorf("reboot failed: %v", err)
	}

	return nil
}

func newPayload() string {
	plog.Info("Generating update payload")

	// check for update file, generate if it doesn't exist
	dir := sdk.BuildImageDir(kola.QEMUOptions.Board, "latest")
	if err := sdkomaha.GenerateFullUpdate(dir); err != nil {
		plog.Fatalf("Building full update failed: %v", err)
	}

	return filepath.Join(dir, "coreos_production_update.gz")
}

func newUserdata(qc *qemu.Cluster) (string, error) {
	keys, err := qc.Keys()
	if err != nil {
		return "", err
	}

	params := userdataParams{
		Port: qc.OmahaServer.Addr().(*net.TCPAddr).Port,
		Keys: keys,
	}
	tmpl, err := template.New("userdata").Parse(userdataTmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &params); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func startJournal(m platform.Machine) error {
	if plog.LevelAt(capnslog.DEBUG) {
		if err := platform.StreamJournal(m); err != nil {
			return fmt.Errorf("start journal: %v", err)
		}
	}
	return nil
}

func checkUsrA(m platform.Machine) error {
	plog.Info("Checking for boot from USR-A partition")
	return checkUsrPartition(m, []string{
		"PARTUUID=" + sdk.USRAUUID.String(),
		"PARTLABEL=USR-A"})
}

func checkUsrB(m platform.Machine) error {
	plog.Info("Checking for boot from USR-B partition")
	return checkUsrPartition(m, []string{
		"PARTUUID=" + sdk.USRBUUID.String(),
		"PARTLABEL=USR-B"})
}

// checkUsrPartition inspects /proc/cmdline of the machine, looking for the
// expected partition mounted at /usr.
func checkUsrPartition(m platform.Machine, accept []string) error {
	out, err := m.SSH("cat /proc/cmdline")
	if err != nil {
		return fmt.Errorf("cat /proc/cmdline: %v: %v", out, err)
	}
	plog.Debugf("Kernel cmdline: %s", out)

	vars := splitSpaceEnv(string(out))
	for _, a := range accept {
		if vars["mount.usr"] == a {
			return nil
		}
		if vars["verity.usr"] == a {
			return nil
		}
		if vars["usr"] == a {
			return nil
		}
	}

	return fmt.Errorf("mount.usr not one of %q", strings.Join(accept, " "))
}

// split space-seperated KEY=VAL pairs into a map
func splitSpaceEnv(envs string) map[string]string {
	m := make(map[string]string)
	pairs := strings.Fields(envs)
	for _, p := range pairs {
		spl := strings.SplitN(p, "=", 2)
		if len(spl) == 2 {
			m[spl[0]] = spl[1]
		}
	}
	return m
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
