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

	"github.com/coreos/pkg/capnslog"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform")
)

type BaseCluster struct {
	agent *network.SSHAgent

	machlock   sync.Mutex
	machmap    map[string]Machine
	consolemap map[string]string

	name       string
	rconf      *RuntimeConfig
	platform   Name
	ctPlatform string
	baseopts   *Options
}

func NewBaseCluster(opts *Options, rconf *RuntimeConfig, platform Name, ctPlatform string) (*BaseCluster, error) {
	return NewBaseClusterWithDialer(opts, rconf, platform, ctPlatform, network.NewRetryDialer())
}

func NewBaseClusterWithDialer(opts *Options, rconf *RuntimeConfig, platform Name, ctPlatform string, dialer network.Dialer) (*BaseCluster, error) {
	agent, err := network.NewSSHAgent(dialer)
	if err != nil {
		return nil, err
	}

	bc := &BaseCluster{
		agent:      agent,
		machmap:    make(map[string]Machine),
		consolemap: make(map[string]string),
		name:       fmt.Sprintf("%s-%s", opts.BaseName, uuid.NewV4()),
		rconf:      rconf,
		platform:   platform,
		ctPlatform: ctPlatform,
		baseopts:   opts,
	}

	return bc, nil
}

func (bc *BaseCluster) SSHClient(ip string) (*ssh.Client, error) {
	sshClient, err := bc.agent.NewClient(ip)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) UserSSHClient(ip, user string) (*ssh.Client, error) {
	sshClient, err := bc.agent.NewUserClient(ip, user)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) PasswordSSHClient(ip string, user string, password string) (*ssh.Client, error) {
	sshClient, err := bc.agent.NewPasswordClient(ip, user, password)
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
	return bc.agent.List()
}

func (bc *BaseCluster) RenderUserData(userdata *conf.UserData, ignitionVars map[string]string) (*conf.Conf, error) {
	if userdata == nil {
		userdata = conf.Ignition(`{"ignition": {"version": "2.0.0"}}`)
	}

	// hacky solution for unified ignition metadata variables
	if userdata.IsIgnitionCompatible() {
		for k, v := range ignitionVars {
			userdata = userdata.Subst(k, v)
		}
	}

	conf, err := userdata.Render(bc.ctPlatform)
	if err != nil {
		return nil, err
	}

	if !bc.rconf.NoSSHKeyInUserData {
		keys, err := bc.Keys()
		if err != nil {
			return nil, err
		}

		conf.CopyKeys(keys)
	}

	return conf, nil
}

// Destroy destroys each machine in the cluster and closes the SSH agent.
func (bc *BaseCluster) Destroy() {

	for _, m := range bc.Machines() {
		m.Destroy()
	}

	if err := bc.agent.Close(); err != nil {
		plog.Errorf("Error closing agent: %v", err)
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

func (bc *BaseCluster) Platform() Name {
	return bc.platform
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
