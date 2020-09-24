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
	localSecurityClientV3 = conf.Ignition(`{
        "ignition": {
            "version": "3.0.0",
            "config": {
                "merge": [{
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
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.security.tls",
		Run:         securityTLS,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"TLSServe":   register.CreateNativeFuncWrap(TLSServe),
			"TLSServeV3": register.CreateNativeFuncWrap(TLSServeV3),
		},
		Tags: []string{"ignition"},
		// DO: https://github.com/coreos/bugs/issues/2205
		// Packet & QEMU: https://github.com/coreos/ignition/issues/645
		ExcludePlatforms: []string{"do", "packet", "qemu"},
	})
}

func securityTLS(c cluster.TestCluster) {
	server := c.Machines()[0]

	ip := server.PrivateIP()
	if c.Platform() == packet.Platform {
		// private IP not configured in the initramfs
		ip = server.IP()
	}

	c.MustSSH(server, "sudo mkdir /var/tls")
	c.MustSSH(server, "sudo openssl ecparam -genkey -name secp384r1 -out /var/tls/server.key")
	c.MustSSH(server, strings.Replace(`sudo bash -c 'openssl req -new -x509 -sha256 -key /var/tls/server.key -out /var/tls/server.crt -days 3650 -subj "/CN=$IP" -config <(cat <<-EOF
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
	publicKey := c.MustSSH(server, "sudo cat /var/tls/server.crt")

	var serveFunc string
	var conf *conf.UserData
	switch c.IgnitionVersion() {
	case "v2":
		serveFunc = "TLSServe"
		conf = localSecurityClient
	case "v3":
		serveFunc = "TLSServeV3"
		conf = localSecurityClientV3
	default:
		c.Fatal("unknown ignition version")
	}

	c.MustSSH(server, fmt.Sprintf("sudo systemd-run --quiet ./kolet run %s %s", c.H.Name(), serveFunc))

	client, err := c.NewMachine(conf.Subst("$IP", ip).Subst("$KEY", dataurl.EncodeBytes(publicKey)))
	if err != nil {
		c.Fatalf("starting client: %v", err)
	}

	checkResources(c, client, map[string]string{
		"data": "kola-data",
	})
}

func ServeTLS(customFile []byte) error {
	publicKey, err := ioutil.ReadFile("/var/tls/server.crt")
	if err != nil {
		return fmt.Errorf("reading public key: %v", err)
	}

	privateKey, err := ioutil.ReadFile("/var/tls/server.key")
	if err != nil {
		return fmt.Errorf("reading private key: %v", err)
	}

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

func TLSServe() error {
	customFile := []byte(`{
        "ignition": { "version": "2.1.0" },
        "storage": {
            "files": [{
                "filesystem": "root",
                "path": "/var/resource/data",
                "contents": { "source": "data:,kola-data" }
            }]
        }
    }`)
	return ServeTLS(customFile)
}

func TLSServeV3() error {
	customFileV3 := []byte(`{
        "ignition": { "version": "3.0.0" },
        "storage": {
            "files": [{
                "path": "/var/resource/data",
                "contents": { "source": "data:,kola-data" }
            }]
        }
    }`)
	return ServeTLS(customFileV3)
}
