// Copyright 2025 Red Hat
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

package kubevirt

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/rest"
)

// PortForwardTunnel manages a local TCP listener that tunnels connections
// to a KubeVirt VMI port via the portforward subresource using WebSocket.
type PortForwardTunnel struct {
	// LocalAddr is the local address (e.g., "127.0.0.1:54321") that
	// can be used for SSH connections.
	LocalAddr string

	listener   net.Listener
	restConfig *rest.Config
	namespace  string
	vmiName    string
	targetPort int
	done       chan struct{}
	closeOnce  sync.Once
}

// StartPortForward creates a local TCP listener that tunnels connections
// to the specified port on the given VMI via the KubeVirt portforward
// subresource API using WebSocket.
func (a *API) StartPortForward(vmiName string, targetPort int) (*PortForwardTunnel, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create local listener: %v", err)
	}

	tunnel := &PortForwardTunnel{
		LocalAddr:  listener.Addr().String(),
		listener:   listener,
		restConfig: a.config,
		namespace:  a.opts.Namespace,
		vmiName:    vmiName,
		targetPort: targetPort,
		done:       make(chan struct{}),
	}

	go tunnel.acceptLoop()

	plog.Infof("Port-forward tunnel listening on %s -> %s/%s:%d", tunnel.LocalAddr, a.opts.Namespace, vmiName, targetPort)
	return tunnel, nil
}

// Stop closes the tunnel listener and stops accepting new connections.
func (t *PortForwardTunnel) Stop() {
	t.closeOnce.Do(func() {
		close(t.done)
		t.listener.Close()
	})
}

func (t *PortForwardTunnel) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.done:
				return
			default:
				plog.Errorf("port-forward accept error: %v", err)
				continue
			}
		}
		go t.handleConnection(conn)
	}
}

func (t *PortForwardTunnel) handleConnection(conn net.Conn) {
	defer conn.Close()

	wsConn, err := t.dialWebSocket()
	if err != nil {
		plog.Errorf("failed to dial websocket portforward: %v", err)
		return
	}
	defer wsConn.Close()

	// Bidirectional copy between local TCP connection and WebSocket
	var wg sync.WaitGroup
	wg.Add(2)

	// WebSocket -> local TCP
	go func() {
		defer wg.Done()
		for {
			_, reader, err := wsConn.NextReader()
			if err != nil {
				return
			}
			if _, err := io.Copy(conn, reader); err != nil {
				return
			}
		}
	}()

	// local TCP -> WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				// Send close message to signal EOF
				wsConn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
		}
	}()

	wg.Wait()
}

// dialWebSocket establishes a WebSocket connection to the KubeVirt
// portforward subresource endpoint.
// URL format: wss://{host}/apis/subresources.kubevirt.io/v1/namespaces/{ns}/virtualmachineinstances/{name}/portforward/{port}/tcp
func (t *PortForwardTunnel) dialWebSocket() (*websocket.Conn, error) {
	hostURL, err := url.Parse(t.restConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing host URL: %v", err)
	}

	// Convert scheme to WebSocket
	scheme := "wss"
	if hostURL.Scheme == "http" {
		scheme = "ws"
	}

	wsURL := url.URL{
		Scheme: scheme,
		Host:   hostURL.Host,
		Path: fmt.Sprintf("/apis/subresources.kubevirt.io/v1/namespaces/%s/virtualmachineinstances/%s/portforward/%d/tcp",
			t.namespace, t.vmiName, t.targetPort),
	}

	// Build TLS config from rest.Config
	tlsConfig, err := rest.TLSConfigFor(t.restConfig)
	if err != nil {
		return nil, fmt.Errorf("building TLS config: %v", err)
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}

	dialer := websocket.Dialer{
		TLSClientConfig: tlsConfig,
		Subprotocols:    []string{plainStreamProtocol},
	}

	// Add auth headers
	headers := http.Header{}
	if t.restConfig.BearerToken != "" {
		headers.Set("Authorization", "Bearer "+t.restConfig.BearerToken)
	}

	plog.Debugf("Dialing WebSocket: %s", wsURL.String())
	wsConn, resp, err := dialer.Dial(wsURL.String(), headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %v", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %v", err)
	}

	return wsConn, nil
}

const (
	// plainStreamProtocol is the WebSocket subprotocol used by KubeVirt
	// for plain byte-stream port forwarding.
	plainStreamProtocol = "plain.kubevirt.io"
)
