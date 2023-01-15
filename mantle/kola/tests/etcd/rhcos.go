// Copyright 2018 Red Hat, Inc.
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

package etcd

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "kola/tests/etcd")

func init() {
	register.RegisterTest(&register.Test{
		Run:         rhcosClusterInsecure,
		ClusterSize: 3,
		Name:        "rhcos.etcd.cluster.insecure",
		UserData: conf.Ignition(`{
  "ignition": { "version": "3.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd.service",
        "enable": false,
        "contents": "[Unit]\nDescription=etcd in podman\nWants=network-online.target\nAfter=network-online.target\n\n[Service]\nType=simple\nRestart=on-failure\nRestartSec=10s\nExecStart=/usr/bin/podman run --name=etcd --net=host quay.io/coreos/etcd /usr/local/bin/etcd --name=${NODE_NAME} --initial-advertise-peer-urls=http://${NODE_IP}:2380 --listen-peer-urls=http://${NODE_IP}:2380 --advertise-client-urls=http://${NODE_IP}:2379 --listen-client-urls=http://${NODE_IP}:2379,http://127.0.0.1:2379 --initial-cluster=${CLUSTER} --initial-cluster-state=new --initial-cluster-token=${TOKEN}\n\n[Install]\nWantedBy=multi-user.target\n",
        "dropins": [{
          "name": "cluster.conf",
          "contents": "# placeholder"
        }]
      }
    ]
  }
}`),
		Flags:   []register.Flag{register.RequiresInternetAccess}, // fetching etcd requires networking
		Distros: []string{"rhcos"},
		// qemu-unpriv machines cannot communicate between each other
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
	register.RegisterTest(&register.Test{
		Run:         rhcosClusterTLS,
		ClusterSize: 3,
		Name:        "rhcos.etcd.cluster.tls",
		UserData: conf.Ignition(`{
  "ignition": { "version": "3.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd.service",
        "enable": false,
        "contents": "[Unit]\nDescription=etcd in podman\nWants=network-online.target\nAfter=network-online.target\n\n[Service]\nType=simple\nRestart=on-failure\nRestartSec=10s\nExecStart=/usr/bin/podman run --name=etcd --net=host --security-opt=label=disable --volume=/etc/ssl/certs:/etc/ssl/certs:ro quay.io/coreos/etcd /usr/local/bin/etcd --name=${NODE_NAME} --initial-advertise-peer-urls=https://${NODE_IP}:2380 --listen-peer-urls=https://${NODE_IP}:2380 --advertise-client-urls=https://${NODE_IP}:2379 --listen-client-urls=https://${NODE_IP}:2379,http://127.0.0.1:2379 --initial-cluster=${CLUSTER} --initial-cluster-state=new --initial-cluster-token=${TOKEN} --cert-file=/etc/ssl/certs/etcd-cert.pem --key-file=/etc/ssl/certs/etcd-key.pem --peer-cert-file=/etc/ssl/certs/peer-cert.pem --peer-key-file=/etc/ssl/certs/peer-key.pem --peer-client-cert-auth --peer-trusted-ca-file=/etc/ssl/certs/ca-peer-cert.pem\n\n[Install]\nWantedBy=multi-user.target\n",
        "dropins": [{
          "name": "cluster.conf",
          "contents": "# placeholder"
        }]
      }
    ]
  },
  "storage": {
    "files": [
      {
        "path": "/etc/ssl/etcd.cnf",
        "contents": { "source": "data:,%5Breq%5D%0Adistinguished_name=req%0A%5Betcd_ca%5D%0AbasicConstraints=CA:true%0AkeyUsage=keyCertSign,cRLSign%0AsubjectKeyIdentifier=hash%0A%5Betcd_peer%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=clientAuth,serverAuth%0AkeyUsage=digitalSignature,keyEncipherment%0AsubjectAltName=@sans%0A%5Betcd_server%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=serverAuth%0AkeyUsage=digitalSignature,keyEncipherment%0AsubjectAltName=@sans%0A%5Bsans%5D%0ADNS.1=localhost%0AIP.1=127.0.0.1%0A" },
        "mode": 420
      }
    ]
  }
}`),
		Flags:   []register.Flag{register.RequiresInternetAccess}, // fetching etcd requires networking
		Distros: []string{"rhcos"},
		// qemu-unpriv machines cannot communicate between each other
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
}

// Run an etcd cluster in podman without TLS or external discovery services.
// Verify it works by checking the cluster health, then writing/reading keys.
func rhcosClusterInsecure(c cluster.TestCluster) {
	machines := c.Machines()

	// Generate the initial cluster value.
	cluster := ""
	for index, machine := range machines {
		cluster += fmt.Sprintf(",etcd%d=http://%s:2380", index, machine.PrivateIP())
	}
	cluster = cluster[1:]

	// Configure the cluster nodes, and start them.
	for index, machine := range machines {
		c.RunCmdSyncf(machine, `set -e ; exec 2>&1
sudo tee /etc/systemd/system/etcd.service.d/cluster.conf << 'EOF' > /dev/null
[Service]
Environment="NODE_NAME=etcd%d"
Environment="NODE_IP=%s"
Environment="CLUSTER=%s"
Environment="TOKEN=etcd-cluster-token"
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now etcd`, index, machine.PrivateIP(), cluster)
	}

	// Test the reported cluster health.
	if err := rhcosClusterHealth(c); err != nil {
		c.Fatalf("discovery failed cluster-health check: %v", err)
	}

	// Test writing keys with curl over local HTTP.
	var keyMap map[string]string
	keyMap, err := setKeys(c, 5)
	if err != nil {
		c.Fatalf("failed to set keys: %v", err)
	}

	// Test reading keys with curl over local HTTP.
	if err := checkKeys(c, keyMap); err != nil {
		c.Fatalf("failed to check keys: %v", err)
	}
}

// Run an etcd cluster in podman with TLS without discovery services.
// Verify it works by checking the cluster health, then writing/reading keys.
// Keys are written/read to the local node over plain HTTP so etcdctl commands
// don't need cert args.  There are separate commands to test writing/reading
// keys over HTTPS with the nodes' external IP addresses.  Communication
// between peers uses TLS with both client and server authentication, but etcd
// clients do not need to use cert auth.
func rhcosClusterTLS(c cluster.TestCluster) {
	machines := c.Machines()

	// Generate the initial cluster value.
	cluster := ""
	for index, machine := range machines {
		cluster += fmt.Sprintf(",etcd%d=https://%s:2380", index, machine.PrivateIP())
	}
	cluster = cluster[1:]

	// Create the CA certs and keys.
	rhcosClusterCreateCAFiles(c)

	// Configure the cluster nodes, and start them.
	for index, machine := range machines {
		c.RunCmdSyncf(machine, `set -e ; exec 2>&1
echo -e 'DNS.2=etcd%d\nIP.2=%s' | sudo tee -a /etc/ssl/etcd.cnf > /dev/null
sudo openssl req -config /etc/ssl/etcd.cnf -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_server -subj '/CN=etcd%d' -out /etc/ssl/certs/etcd-cert-self.pem -keyout /etc/ssl/certs/etcd-key.pem
sudo openssl req -config /etc/ssl/etcd.cnf -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_peer -subj '/CN=etcd%d peer' -out /etc/ssl/certs/peer-cert-self.pem -keyout /etc/ssl/certs/peer-key.pem
sudo openssl x509 -CA /etc/ssl/certs/ca-etcd-cert.pem -CAkey /etc/ssl/certs/ca-etcd-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/etcd-cert-self.pem -out /etc/ssl/certs/etcd-cert.pem
sudo openssl x509 -CA /etc/ssl/certs/ca-peer-cert.pem -CAkey /etc/ssl/certs/ca-peer-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/peer-cert-self.pem -out /etc/ssl/certs/peer-cert.pem
sudo tee /etc/systemd/system/etcd.service.d/cluster.conf << 'EOF' > /dev/null
[Service]
Environment="NODE_NAME=etcd%d"
Environment="NODE_IP=%s"
Environment="CLUSTER=%s"
Environment="TOKEN=etcd-cluster-token"
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now etcd`, index, machine.PrivateIP(), index, index, index, machine.PrivateIP(), cluster)
	}

	// Test the reported cluster health.
	if err := rhcosClusterHealth(c); err != nil {
		c.Fatalf("discovery failed cluster-health check: %v", err)
	}

	// Verify writing and reading keys over TLS.
	c.RunCmdSyncf(machines[1], "curl -sk https://%s:2379/v2/keys/kolavar -XPUT -d value=kolavalue", machines[1].PrivateIP())
	c.RunCmdSyncf(machines[2], "curl -sk https://%s:2379/v2/keys/kolavar?quorum=true | grep -Fq kolavalue", machines[2].PrivateIP())

	// Test writing keys with curl over local HTTP.
	var keyMap map[string]string
	keyMap, err := setKeys(c, 5)
	if err != nil {
		c.Fatalf("failed to set keys: %v", err)
	}

	// Test reading keys with curl over local HTTP.
	if err := checkKeys(c, keyMap); err != nil {
		c.Fatalf("failed to check keys: %v", err)
	}
}

// Generate the shared CA certificates and keys on a node, and copy them to the
// other nodes in the cluster to sign their certs.  Yes, you would normally
// copy the nodes' certificates to the system with the CA's private keys to
// sign them, but we don't care about these temporary keys, and this results in
// less file transfers between hosts.
func rhcosClusterCreateCAFiles(c cluster.TestCluster) {
	caNode := c.Machines()[0]

	// Generate CA certificates on one node.
	c.RunCmdSync(caNode, `set -e ; exec 2>&1
sudo mkdir -p /etc/ssl/certs
sudo openssl req -config /etc/ssl/etcd.cnf -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=etcd CA' -out /etc/ssl/certs/ca-etcd-cert.pem -keyout /etc/ssl/certs/ca-etcd-key.pem
sudo openssl req -config /etc/ssl/etcd.cnf -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=peer CA' -out /etc/ssl/certs/ca-peer-cert.pem -keyout /etc/ssl/certs/ca-peer-key.pem`)

	// Collect and compress the CA files to send to the other nodes.
	tar, err := c.SSH(caNode, "sudo tar -C /etc/ssl/certs -cJ ca-{etcd,peer}-{cert,key}.pem")
	if err != nil {
		c.Fatalf("failed collecting CA files: %v", err)
	}

	for _, machine := range c.Machines() {
		if machine == caNode {
			continue
		}

		client, err := machine.SSHClient()
		if err != nil {
			c.Fatalf("failed creating SSH client: %v", err)
		}
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			c.Fatalf("failed creating SSH session: %v", err)
		}
		defer session.Close()

		session.Stdin = bytes.NewReader(tar)
		out, err := session.CombinedOutput("sudo mkdir -p /etc/ssl/certs && sudo tar -C /etc/ssl/certs -xJ")
		if err != nil {
			c.Fatalf("failed extracting CA files: %q: %v", out, err)
		}
	}
}

// This is basically GetClusterHealth, but it needs to enter the container to
// call the etcdctl command.  It also specifies the etcd3 API.
func rhcosClusterHealth(c cluster.TestCluster) error {
	var b []byte
	machine := c.Machines()[0]
	csize := len(c.Machines())

	checker := func() error {
		b, err := c.SSH(machine, "sudo podman exec --env=ETCDCTL_API=3 etcd /usr/local/bin/etcdctl endpoint --cluster health 2>&1")
		if err != nil {
			return err
		}

		// The repsonse should include "healthy" for each machine.
		if strings.Count(string(b), "is healthy") != csize {
			return fmt.Errorf("unexpected etcdctl output")
		}

		plog.Infof("cluster healthy")
		return nil
	}

	if err := util.Retry(15, 10*time.Second, checker); err != nil {
		return fmt.Errorf("health polling failed: %v: %s", err, b)
	}

	return nil
}
