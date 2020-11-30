// Copyright 2017 CoreOS, Inc.
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

// mockssh implements a basic ssh server for use in unit tests.
//
// Command execution in the server is implemented by a user provided handler
// function rather than executing a real shell.
//
// Some inspiration is taken from but not based on:
// https://godoc.org/github.com/gliderlabs/ssh
package mockssh

import (
	"fmt"
	"io"
	"log"
	"net"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/network/bufnet"
)

const (
	pipeBufferSize = 8192
)

var (
	mockServerPrivateKey ssh.Signer
)

func init() {
	var err error
	mockServerPrivateKey, err = ssh.ParsePrivateKey([]byte(`
-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQC+6EsrBSr0Ik+ADcR17zjYK9+RcO+AYFA6IN/wYl0lV8Os/Md8
amVUnC3FGhvK4hQwvQVEQpoxcT4DoHZh6Fs4uStixFZSCWLGbYwb8qsRkMJl/ZkZ
kgY/ZUOQSHqoNsIkVajoVPOK8gb9pFcDW0WHcIDHa6L+IoZH7nfUmG5/9QIDAQAB
AoGBALxhwPr8qHwr90MnUrwFiZRXBtAgH1YQtFoH4rL0fXHB/wcOkVMGMmOhkdCz
iMVU/hNyEmZfSoSLeGRfzTGj9Y541nfcbFcCwpen8mfLk4JyVsHr1J9T/c0i9yot
NtZFU6Imsw6judu4ohzLrI6hYdvSTUzJvrUe4jKQ8uv/O4JBAkEA6ON3ZnxlwtvC
rcTBes/8bHLjrvQk371HraRH9xN29XSII11igPYDGRsrO8+5fTcVi/gYI6GIo/pU
amRoMgwm7QJBANHaS9VJwSWHyfO5AjNHzOQM7M5SUf9KVTUdgCXi+H0cBPZdlZaF
FviXHnH114tiSlKDmwJicrmWW0Pk0c1A1CkCQGoWZGe9NyXisfYycOifIh/M3kbu
VHXPZX2GHnpA1anOoc1qVtrkNlkTdUhTwe12UExogaaJiRMZj6a/gm959akCQQCo
KXsdRsYNMhwmPzpBJ6dLlAPrbdIhdkqDjslTEue3Mc3UMrgdbzcyK78M6Uk5e6E9
MBL2PTfb+l3WMTXiebHJAkEApUjV9xL1i+7EidI3hOgTZxk5Ti6eXpZdjQIN3OGn
uCeD0x31tIEl6p5wYaspSAOZJh8/jz4qMbLOUjmhRUzIJg==
-----END RSA PRIVATE KEY-----
`))
	if err != nil {
		panic(err)
	}
}

// SessionHandler is a user provided function that processes/executes
// the command given to it in the given Session. Before finishing the
// handler must call session.Close or session.Exit.
type SessionHandler func(session *Session)

// Session represents the server side execution of the client's ssh.Session.
type Session struct {
	Exec   string   // Command to execute.
	Env    []string // Environment values provided by the client.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	channel ssh.Channel
}

// Exit sends the given command exit status and closes the session.
func (s *Session) Exit(code int) error {
	status := struct{ Status uint32 }{uint32(code)}
	payload := ssh.Marshal(&status)

	_, err := s.channel.SendRequest("exit-status", false, payload)
	if err != nil {
		return err
	}

	return s.channel.Close()
}

// Close ends the session without sending an exit status.
func (s *Session) Close() error {
	return s.channel.Close()
}

// NewMockClient starts a ssh server backed by the given handler and
// returns a client that is connected to it.
func NewMockClient(handler SessionHandler) *ssh.Client {
	config := ssh.ClientConfig{
		User: "mock",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	pipe := startMockServer(handler)
	conn, chans, reqs, err := ssh.NewClientConn(pipe, "mock", &config)
	if err != nil {
		panic(err)
	}

	return ssh.NewClient(conn, chans, reqs)
}

type mockServer struct {
	config  ssh.ServerConfig
	handler SessionHandler
	server  *ssh.ServerConn
}

func startMockServer(handler SessionHandler) net.Conn {
	m := mockServer{
		config: ssh.ServerConfig{
			PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
				return nil, nil
			},
		},
		handler: handler,
	}
	m.config.AddHostKey(mockServerPrivateKey)

	sPipe, cPipe := bufnet.FixedPipe(pipeBufferSize)
	go m.handleServerConn(sPipe)
	return cPipe
}

func (m *mockServer) handleServerConn(conn net.Conn) {
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, &m.config)
	if err != nil {
		log.Printf("mockssh: server handshake failed: %v", err)
		return
	}
	m.server = serverConn

	// reqs must be serviced but are not important to us.
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		go m.handleServerChannel(newChannel)
	}
}

func (m *mockServer) handleServerChannel(newChannel ssh.NewChannel) {
	if newChannel.ChannelType() != "session" {
		_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		return
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("mockssh: accepting channel failed: %v", err)
		return
	}

	session := &Session{
		Stdin:   channel,
		Stdout:  channel,
		Stderr:  channel.Stderr(),
		channel: channel,
	}

	// shell and pty requests are not implemented.
	for req := range requests {
		if session == nil {
			_ = req.Reply(false, nil)
		}
		switch req.Type {
		case "exec":
			v := struct{ Value string }{}
			if err := ssh.Unmarshal(req.Payload, &v); err != nil {
				_ = req.Reply(false, nil)
			} else {
				session.Exec = v.Value
				_ = req.Reply(true, nil)
				go m.handler(session)
			}
			session = nil
		case "env":
			kv := struct{ Key, Value string }{}
			if err := ssh.Unmarshal(req.Payload, &kv); err != nil {
				_ = req.Reply(false, nil)
			} else {
				env := fmt.Sprintf("%s=%s", kv.Key, kv.Value)
				session.Env = append(session.Env, env)
				_ = req.Reply(true, nil)
			}
		default:
			_ = req.Reply(false, nil)
		}
	}
}
