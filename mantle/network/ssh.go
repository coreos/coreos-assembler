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

package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	defaultPort = 22
	defaultUser = "core"
)

// DefaultSSHDir is a process-global path that can be set, and
// currently is by kola.
var DefaultSSHDir string

// Dialer is an interface for anything compatible with net.Dialer
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

// SSHAgent can manage keys, updates cloud config, and loves ponies.
// The embedded dialer is used for establishing new SSH connections.
type SSHAgent struct {
	agent.Agent
	Dialer
	User         string
	Socket       string
	sockDir      string
	sockdirOwned bool
	listener     *net.UnixListener
}

// NewSSHAgent constructs a new SSHAgent using dialer to create ssh
// connections.
func NewSSHAgent(dialer Dialer) (*SSHAgent, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	addedkey := agent.AddedKey{
		PrivateKey: key,
		Comment:    "core@default",
	}

	keyring := agent.NewKeyring()
	err = keyring.Add(addedkey)
	if err != nil {
		return nil, err
	}

	var sockDir string
	var ok, sockdirOwned bool
	if sockDir, ok = os.LookupEnv("MANTLE_SSH_DIR"); !ok {
		if DefaultSSHDir == "" {
			sockDir, err = ioutil.TempDir("", "mantle-ssh-")
			sockdirOwned = true
			if err != nil {
				return nil, err
			}
		} else {
			sockDir = DefaultSSHDir
		}
	}

	// Use a similar naming scheme to ssh-agent
	sockPath := fmt.Sprintf("%s/agent.%d", sockDir, os.Getpid())
	sockAddr := &net.UnixAddr{Name: sockPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", sockAddr)
	if err != nil {
		if sockdirOwned {
			os.RemoveAll(sockDir)
		}
		return nil, err
	}

	a := &SSHAgent{
		Agent:        keyring,
		Dialer:       dialer,
		User:         defaultUser,
		Socket:       sockPath,
		sockDir:      sockDir,
		sockdirOwned: sockdirOwned,
		listener:     listener,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = agent.ServeAgent(a, conn)
			}()
		}
	}()

	return a, nil
}

// Close closes the unix socket of the agent.
func (a *SSHAgent) Close() error {
	a.listener.Close()
	if a.sockdirOwned {
		return os.RemoveAll(a.sockDir)
	}
	return nil
}

// Add port to host if not already set.
func ensurePortSuffix(host string, port int) string {
	switch {
	case !strings.Contains(host, ":"):
		return fmt.Sprintf("%s:%d", host, port)
	case strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]"):
		return fmt.Sprintf("%s:%d", host, port)
	case strings.HasPrefix(host, "[") && strings.Contains(host, "]:"):
		return host
	case strings.Count(host, ":") > 1:
		return fmt.Sprintf("[%s]:%d", host, port)
	default:
		return host
	}
}

func (a *SSHAgent) newClient(host string, user string, auth []ssh.AuthMethod) (*ssh.Client, error) {
	sshcfg := ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	addr := ensurePortSuffix(host, defaultPort)
	tcpconn, err := a.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	sshconn, chans, reqs, err := ssh.NewClientConn(tcpconn, addr, &sshcfg)
	if err != nil {
		return nil, err
	}

	client := ssh.NewClient(sshconn, chans, reqs)
	err = agent.ForwardToAgent(client, a)
	if err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}

// NewClient connects to the given host via SSH, the client will support
// agent forwarding but it must also be enabled per-session.
func (a *SSHAgent) NewClient(host string) (*ssh.Client, error) {
	return a.NewUserClient(host, a.User)
}

// NewUserClient connects to the given host via SSH using the provided username.
// The client will support agent forwarding but it must also be enabled per-session.
func (a *SSHAgent) NewUserClient(host string, user string) (*ssh.Client, error) {
	client, err := a.newClient(host, user, []ssh.AuthMethod{ssh.PublicKeysCallback(a.Signers)})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// NewPasswordClient connects to the given host via SSH using the
// provided username and password
func (a *SSHAgent) NewPasswordClient(host string, user string, password string) (*ssh.Client, error) {
	client, err := a.newClient(host, user, []ssh.AuthMethod{ssh.Password(password)})
	if err != nil {
		return nil, err
	}
	return client, nil
}
