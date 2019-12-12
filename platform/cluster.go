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

package platform

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/pborman/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	platformConf "github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

type BaseCluster struct {
	machlock   sync.Mutex
	machmap    map[string]Machine
	consolemap map[string]string

	bf    *BaseFlight
	name  string
	rconf *RuntimeConfig
}

func NewBaseCluster(bf *BaseFlight, rconf *RuntimeConfig) (*BaseCluster, error) {
	bc := &BaseCluster{
		bf:         bf,
		machmap:    make(map[string]Machine),
		consolemap: make(map[string]string),
		name:       fmt.Sprintf("%s-%s", bf.baseopts.BaseName, uuid.New()),
		rconf:      rconf,
	}

	return bc, nil
}

func (bc *BaseCluster) SSHClient(ip string) (*ssh.Client, error) {
	sshClient, err := bc.bf.agent.NewClient(ip)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) UserSSHClient(ip, user string) (*ssh.Client, error) {
	sshClient, err := bc.bf.agent.NewUserClient(ip, user)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) PasswordSSHClient(ip string, user string, password string) (*ssh.Client, error) {
	sshClient, err := bc.bf.agent.NewPasswordClient(ip, user, password)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

// SSH executes the given command, cmd, on the given Machine, m. It returns the
// stdout and stderr of the command and an error.
// Leading and trailing whitespace is trimmed from each.
func (bc *BaseCluster) SSH(m Machine, cmd string) ([]byte, []byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	client, err := bc.SSHClient(m.IP())
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()

	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run(cmd)
	outBytes := bytes.TrimSpace(stdout.Bytes())
	errBytes := bytes.TrimSpace(stderr.Bytes())
	return outBytes, errBytes, err
}

func (bc *BaseCluster) Machines() []Machine {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	machs := make([]Machine, 0, len(bc.machmap))
	for _, m := range bc.machmap {
		machs = append(machs, m)
	}
	return machs
}

func (bc *BaseCluster) AddMach(m Machine) {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	bc.machmap[m.ID()] = m
}

func (bc *BaseCluster) DelMach(m Machine) {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	delete(bc.machmap, m.ID())
	bc.consolemap[m.ID()] = m.ConsoleOutput()
}

func (bc *BaseCluster) Keys() ([]*agent.Key, error) {
	return bc.bf.Keys()
}

func (bc *BaseCluster) RenderUserData(userdata *platformConf.UserData, ignitionVars map[string]string) (*platformConf.Conf, error) {
	if userdata == nil {
		switch bc.IgnitionVersion() {
		case "v2":
			userdata = platformConf.Ignition(`{"ignition": {"version": "2.0.0"}}`)
		case "v3":
			userdata = platformConf.Ignition(`{"ignition": {"version": "3.0.0"}}`)
		default:
			return nil, fmt.Errorf("unknown ignition version")
		}
	}

	// hacky solution for unified ignition metadata variables
	if userdata.IsIgnitionCompatible() {
		for k, v := range ignitionVars {
			userdata = userdata.Subst(k, v)
		}
	}

	conf, err := userdata.Render(bc.bf.ctPlatform)
	if err != nil {
		return nil, err
	}

	for _, dropin := range bc.bf.baseopts.SystemdDropins {
		conf.AddSystemdUnitDropin(dropin.Unit, dropin.Name, dropin.Contents)
	}

	if !bc.rconf.NoSSHKeyInUserData {
		keys, err := bc.bf.Keys()
		if err != nil {
			return nil, err
		}

		conf.CopyKeys(keys)
	}

	// disable Zincati & Pinger by default
	if bc.Distribution() == "fcos" {
		conf.AddFile("/etc/fedora-coreos-pinger/config.d/90-disable-reporting.toml", "root", `[reporting]
enabled = false`, 0644)
		conf.AddFile("/etc/zincati/config.d/90-disable-auto-updates.toml", "root", `[updates]
enabled = false`, 0644)
	}

	if bc.bf.baseopts.OSContainer != "" {
		if bc.Distribution() != "rhcos" {
			return nil, fmt.Errorf("oscontainer is only supported on the rhcos distribution")
		}
		conf.AddSystemdUnitDropin("pivot.service", "00-before-sshd.conf", `[Unit]
Before=sshd.service`)
		conf.AddSystemdUnit("pivot.service", "", platformConf.Enable)
		conf.AddSystemdUnit("pivot-write-reboot-needed.service", `[Unit]
Description=Touch /run/pivot/reboot-needed
ConditionFirstBoot=true

[Service]
Type=oneshot
ExecStart=/usr/bin/mkdir -p /run/pivot
ExecStart=/usr/bin/touch /run/pivot/reboot-needed

[Install]
WantedBy=multi-user.target
`, platformConf.Enable)
		conf.AddFile("/etc/pivot/image-pullspec", "root", bc.bf.baseopts.OSContainer, 0644)
	}

	if conf.IsIgnition() {
		if !conf.ValidConfig() {
			return nil, fmt.Errorf("invalid ignition config")
		}
	}

	return conf, nil
}

// Destroy destroys each machine in the cluster.
func (bc *BaseCluster) Destroy() {
	for _, m := range bc.Machines() {
		m.Destroy()
	}
}

// XXX(mischief): i don't really think this belongs here, but it completes the
// interface we've established.
func (bc *BaseCluster) GetDiscoveryURL(size int) (string, error) {
	var result string
	err := util.Retry(3, 5*time.Second, func() error {
		resp, err := http.Get(fmt.Sprintf("https://discovery.etcd.io/new?size=%d", size))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("Discovery service returned %q", resp.Status)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(body)
		return nil
	})
	return result, err
}

func (bc *BaseCluster) Distribution() string {
	return bc.bf.baseopts.Distribution
}

func (bc *BaseCluster) IgnitionVersion() string {
	return bc.bf.baseopts.IgnitionVersion
}

func (bc *BaseCluster) SSHOnTestFailure() bool {
	return bc.bf.baseopts.SSHOnTestFailure
}

func (bc *BaseCluster) Platform() Name {
	return bc.bf.Platform()
}

func (bc *BaseCluster) Name() string {
	return bc.name
}

func (bc *BaseCluster) RuntimeConf() RuntimeConfig {
	return *bc.rconf
}

func (bc *BaseCluster) ConsoleOutput() map[string]string {
	ret := map[string]string{}
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	for k, v := range bc.consolemap {
		ret[k] = v
	}
	return ret
}

func (bc *BaseCluster) JournalOutput() map[string]string {
	ret := map[string]string{}
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	for k, v := range bc.machmap {
		ret[k] = v.JournalOutput()
	}
	return ret
}
