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

package ignition

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/vincent-petithory/dataurl"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/packet"
)

var (
	localSecurityClient = conf.Ignition(`{
        "ignition": {
            "version": "2.2.0",
            "config": {
                "append": [{
                    "source": "https://$IP"
                }]
            },
            "security": {
                "tls": {
                    "certificateAuthorities": [{
                        "source": "$KEY"
                    }]
                }
            }
        }
    }`)
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_2.security.tls",
		Run:         securityTLS,
		ClusterSize: 1,
		NativeFuncs: map[string]func() error{
			"TLSServe": TLSServe,
		},
		// https://github.com/coreos/bugs/issues/2205
		ExcludePlatforms: []string{"do"},
	})
}

func securityTLS(c cluster.TestCluster) {
	server := c.Machines()[0]

	ip := server.PrivateIP()
	if c.Platform() == packet.Platform {
		// private IP not configured in the initramfs
		ip = server.IP()
	}

	c.MustSSH(server, "sudo mkdir /tls")
	c.MustSSH(server, "sudo openssl ecparam -genkey -name secp384r1 -out /tls/server.key")
	c.MustSSH(server, strings.Replace(`sudo bash -c 'openssl req -new -x509 -sha256 -key /tls/server.key -out /tls/server.crt -days 3650 -subj "/CN=$IP" -config <(cat <<-EOF
[req]
default_bits = 2048
default_md = sha256
distinguished_name = dn

[ dn ]
CN = $IP

[ SAN ]
subjectAltName = IP:$IP
EOF
) -extensions SAN'`, "$IP", ip, -1))
	publicKey := c.MustSSH(server, "sudo cat /tls/server.crt")

	c.MustSSH(server, fmt.Sprintf("sudo systemd-run --quiet ./kolet run %s TLSServe", c.Name()))

	client, err := c.NewMachine(localSecurityClient.Subst("$IP", ip).Subst("$KEY", dataurl.EncodeBytes(publicKey)))
	if err != nil {
		c.Fatalf("starting client: %v", err)
	}

	checkResources(c, client, map[string]string{
		"data": "kola-data",
	})
}

func TLSServe() error {
	publicKey, err := ioutil.ReadFile("/tls/server.crt")
	if err != nil {
		return fmt.Errorf("reading public key: %v", err)
	}

	privateKey, err := ioutil.ReadFile("/tls/server.key")
	if err != nil {
		return fmt.Errorf("reading private key: %v", err)
	}

	customFile := []byte(`{
        "ignition": { "version": "2.1.0" },
        "storage": {
            "files": [{
                "filesystem": "root",
                "path": "/resource/data",
                "contents": { "source": "data:,kola-data" }
            }]
        }
    }`)

	cer, err := tls.X509KeyPair(publicKey, privateKey)
	if err != nil {
		return fmt.Errorf("error loading x509 keypair: %v", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cer},
	}

	caserver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(customFile)
	}))
	l, err := net.Listen("tcp", ":443")
	if err != nil {
		return err
	}
	caserver.Listener.Close()
	caserver.Listener = l
	caserver.TLS = config
	caserver.StartTLS()

	select {}
}
