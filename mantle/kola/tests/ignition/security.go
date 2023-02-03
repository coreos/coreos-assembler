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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/vincent-petithory/dataurl"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/packet"
)

var (
	localSecurityClient = conf.Ignition(`{
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
		Description: "Verify that we can fetch ignition with https.",
		Run:         securityTLS,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"TLSServe": register.CreateNativeFuncWrap(TLSServe),
		},
		Tags: []string{"ignition"},
		// QEMU doesn't support multiple VMs communicating with each other.
		ExcludePlatforms: []string{"qemu"},
		Timeout:          20 * time.Minute,
	})
}

func securityTLS(c cluster.TestCluster) {
	server := c.Machines()[0]

	ip := server.PrivateIP()
	if c.Platform() == packet.Platform {
		// private IP not configured in the initramfs
		ip = server.IP()
	}

	c.RunCmdSync(server, "sudo mkdir /var/tls")
	c.RunCmdSync(server, "sudo openssl ecparam -genkey -name secp384r1 -out /var/tls/server.key")
	c.RunCmdSync(server, strings.Replace(`sudo bash -c 'openssl req -new -x509 -sha256 -key /var/tls/server.key -out /var/tls/server.crt -days 3650 -subj "/CN=$IP" -config <(cat <<-EOF
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

	var conf *conf.UserData = localSecurityClient
	c.RunCmdSyncf(server, "sudo systemd-run --quiet ./kolet run %s TLSServe", c.H.Name())

	client, err := c.NewMachine(conf.Subst("$IP", ip).Subst("$KEY", dataurl.EncodeBytes(publicKey)))
	if err != nil {
		c.Fatalf("starting client: %v", err)
	}

	checkResources(c, client, map[string]string{
		"data": "kola-data",
	})
}

func ServeTLS(customFile []byte) error {
	publicKey, err := os.ReadFile("/var/tls/server.crt")
	if err != nil {
		return fmt.Errorf("reading public key: %v", err)
	}

	privateKey, err := os.ReadFile("/var/tls/server.key")
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
		_, _ = w.Write(customFile)
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
        "ignition": { "version": "3.0.0" },
        "storage": {
            "files": [{
                "path": "/var/resource/data",
                "contents": { "source": "data:,kola-data" }
            }]
        }
    }`)
	return ServeTLS(customFile)
}
