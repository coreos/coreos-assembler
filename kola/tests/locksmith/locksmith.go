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

package locksmith

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/coreos/go-semver/semver"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.locksmith.cluster",
		Run:         locksmithCluster,
		ClusterSize: 3,
		UserData: conf.Ignition(`{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd-member.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/lib/coreos/etcd-wrapper --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379 --listen-peer-urls=http://$private_ipv4:2380"
        }]
      }
    ]
  },
  "storage": {
    "files": [{
      "filesystem": "root",
      "path": "/etc/coreos/update.conf",
      "contents": { "source": "data:,REBOOT_STRATEGY=etcd-lock%0A" },
      "mode": 420
    }]
  }
}`),
		ExcludePlatforms: []string{"qemu"}, // etcd-member requires networking
		Distros:          []string{"cl"},
	})
	register.Register(&register.Test{
		Name:        "coreos.locksmith.cluster.etcd2",
		Run:         locksmithCluster,
		ClusterSize: 3,
		UserData: conf.Ignition(`{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "etcd2.service",
        "enable": true,
        "dropins": [{
          "name": "metadata.conf",
          "contents": "[Unit]\nWants=coreos-metadata.service\nAfter=coreos-metadata.service\n\n[Service]\nEnvironmentFile=-/run/metadata/coreos\nExecStart=\nExecStart=/usr/bin/etcd2 --discovery=$discovery --advertise-client-urls=http://$private_ipv4:2379 --initial-advertise-peer-urls=http://$private_ipv4:2380 --listen-client-urls=http://0.0.0.0:2379,http://0.0.0.0:4001 --listen-peer-urls=http://$private_ipv4:2380,http://$private_ipv4:7001"
        }]
      },
      {
        "name": "coreos-metadata.service",
        "dropins": [{
          "name": "qemu.conf",
          "contents": "[Unit]\nConditionKernelCommandLine=coreos.oem.id"
        }]
      }
    ]
  },
  "storage": {
    "files": [{
      "filesystem": "root",
      "path": "/etc/coreos/update.conf",
      "contents": { "source": "data:,REBOOT_STRATEGY=etcd-lock%0A" },
      "mode": 420
    }]
  }
}`),
		EndVersion: semver.Version{Major: 1662},
		Distros:    []string{"cl"},
	})
	register.Register(&register.Test{
		Name:        "coreos.locksmith.reboot",
		Run:         locksmithReboot,
		ClusterSize: 1,
		Distros:     []string{"cl"},
	})
	register.Register(&register.Test{
		Name:        "coreos.locksmith.tls",
		Run:         locksmithTLS,
		ClusterSize: 1,
		UserData: conf.Ignition(`{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "certgen.service",
        "contents": "[Unit]\nAfter=system-config.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStartPre=/usr/bin/mkdir -p /etc/ssl/certs\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=etcd CA' -out /etc/ssl/certs/ca-etcd-cert.pem -keyout /etc/ssl/certs/ca-etcd-key.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_server -subj '/CN=localhost' -out /etc/ssl/certs/etcd-cert-self.pem -keyout /etc/ssl/certs/etcd-key.pem\nExecStart=/usr/bin/openssl x509 -CA /etc/ssl/certs/ca-etcd-cert.pem -CAkey /etc/ssl/certs/ca-etcd-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/etcd-cert-self.pem -out /etc/ssl/certs/etcd-cert.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=locksmith CA' -out /etc/ssl/certs/ca-locksmith-cert.pem -keyout /etc/ssl/certs/ca-locksmith-key.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_client -subj '/CN=locksmith client' -out /etc/ssl/certs/locksmith-cert-self.pem -keyout /etc/ssl/certs/locksmith-key.pem\nExecStart=/usr/bin/openssl x509 -CA /etc/ssl/certs/ca-locksmith-cert.pem -CAkey /etc/ssl/certs/ca-locksmith-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/locksmith-cert-self.pem -out /etc/ssl/certs/locksmith-cert.pem\nExecStart=/usr/bin/chmod 0644 /etc/ssl/certs/ca-etcd-cert.pem /etc/ssl/certs/ca-etcd-key.pem /etc/ssl/certs/ca-locksmith-cert.pem /etc/ssl/certs/ca-locksmith-key.pem /etc/ssl/certs/etcd-cert.pem /etc/ssl/certs/etcd-key.pem /etc/ssl/certs/locksmith-cert.pem /etc/ssl/certs/locksmith-key.pem\nExecStart=/usr/bin/ln -fns ca-etcd-cert.pem /etc/ssl/certs/etcd.pem\nExecStart=/usr/bin/c_rehash"
      },
      {
        "name": "etcd-member.service",
        "dropins": [{
          "name": "environment.conf",
          "contents": "[Unit]\nAfter=certgen.service\nRequires=certgen.service\n[Service]\nEnvironment=ETCD_ADVERTISE_CLIENT_URLS=https://127.0.0.1:2379\nEnvironment=ETCD_LISTEN_CLIENT_URLS=https://127.0.0.1:2379\nEnvironment=ETCD_CERT_FILE=/etc/ssl/certs/etcd-cert.pem\nEnvironment=ETCD_KEY_FILE=/etc/ssl/certs/etcd-key.pem\nEnvironment=ETCD_TRUSTED_CA_FILE=/etc/ssl/certs/ca-locksmith-cert.pem\nEnvironment=ETCD_CLIENT_CERT_AUTH=true"
        }]
      },
      {
        "name": "locksmithd.service",
        "enable": true,
        "dropins": [{
          "name": "environment.conf",
          "contents": "[Unit]\nAfter=etcd-member.service\nRequires=etcd-member.service\n[Service]\nEnvironment=LOCKSMITHD_ETCD_CERTFILE=/etc/ssl/certs/locksmith-cert.pem\nEnvironment=LOCKSMITHD_ETCD_KEYFILE=/etc/ssl/certs/locksmith-key.pem\nEnvironment=LOCKSMITHD_ETCD_CAFILE=/etc/ssl/certs/ca-etcd-cert.pem\nEnvironment=LOCKSMITHD_ENDPOINT=https://localhost:2379\nEnvironment=LOCKSMITHD_REBOOT_WINDOW_START=00:00\nEnvironment=LOCKSMITHD_REBOOT_WINDOW_LENGTH=23h59m"
        }]
      }
    ]
  },
  "storage": {
    "files": [
      {
        "filesystem": "root",
        "path": "/etc/coreos/update.conf",
        "contents": { "source": "data:,REBOOT_STRATEGY=etcd-lock%0A" },
        "mode": 420
      },
      {
        "filesystem": "root",
        "path": "/etc/ssl/openssl.cnf",
        "contents": { "source": "data:,%5Breq%5D%0Adistinguished_name=req%0A%5Betcd_ca%5D%0AbasicConstraints=CA:true%0AkeyUsage=keyCertSign,cRLSign%0AsubjectKeyIdentifier=hash%0A%5Betcd_client%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=clientAuth%0AkeyUsage=digitalSignature,keyEncipherment%0A%5Betcd_server%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=serverAuth%0AkeyUsage=digitalSignature,keyEncipherment%0AsubjectAltName=@sans%0A%5Bsans%5D%0ADNS.1=localhost%0AIP.1=127.0.0.1%0A" },
        "mode": 420
      }
    ]
  }
}`),
		ExcludePlatforms: []string{"qemu"}, // etcd-member requires networking
		Distros:          []string{"cl"},
	})
	register.Register(&register.Test{
		Name:        "coreos.locksmith.tls.etcd2",
		Run:         locksmithTLS,
		ClusterSize: 1,
		UserData: conf.Ignition(`{
  "ignition": { "version": "2.0.0" },
  "systemd": {
    "units": [
      {
        "name": "certgen.service",
        "contents": "[Unit]\nAfter=system-config.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStartPre=/usr/bin/mkdir -p /etc/ssl/certs\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=etcd CA' -out /etc/ssl/certs/ca-etcd-cert.pem -keyout /etc/ssl/certs/ca-etcd-key.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_server -subj '/CN=localhost' -out /etc/ssl/certs/etcd-cert-self.pem -keyout /etc/ssl/certs/etcd-key.pem\nExecStart=/usr/bin/openssl x509 -CA /etc/ssl/certs/ca-etcd-cert.pem -CAkey /etc/ssl/certs/ca-etcd-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/etcd-cert-self.pem -out /etc/ssl/certs/etcd-cert.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_ca -subj '/CN=locksmith CA' -out /etc/ssl/certs/ca-locksmith-cert.pem -keyout /etc/ssl/certs/ca-locksmith-key.pem\nExecStart=/usr/bin/openssl req -x509 -nodes -newkey rsa:4096 -sha512 -days 3 -extensions etcd_client -subj '/CN=locksmith client' -out /etc/ssl/certs/locksmith-cert-self.pem -keyout /etc/ssl/certs/locksmith-key.pem\nExecStart=/usr/bin/openssl x509 -CA /etc/ssl/certs/ca-locksmith-cert.pem -CAkey /etc/ssl/certs/ca-locksmith-key.pem -CAcreateserial -sha512 -days 3 -in /etc/ssl/certs/locksmith-cert-self.pem -out /etc/ssl/certs/locksmith-cert.pem\nExecStart=/usr/bin/chmod 0644 /etc/ssl/certs/ca-etcd-cert.pem /etc/ssl/certs/ca-etcd-key.pem /etc/ssl/certs/ca-locksmith-cert.pem /etc/ssl/certs/ca-locksmith-key.pem /etc/ssl/certs/etcd-cert.pem /etc/ssl/certs/etcd-key.pem /etc/ssl/certs/locksmith-cert.pem /etc/ssl/certs/locksmith-key.pem\nExecStart=/usr/bin/ln -fns ca-etcd-cert.pem /etc/ssl/certs/etcd.pem\nExecStart=/usr/bin/c_rehash"
      },
      {
        "name": "etcd2.service",
        "dropins": [{
          "name": "environment.conf",
          "contents": "[Unit]\nAfter=certgen.service\nRequires=certgen.service\n[Service]\nEnvironment=ETCD_ADVERTISE_CLIENT_URLS=https://127.0.0.1:2379\nEnvironment=ETCD_LISTEN_CLIENT_URLS=https://127.0.0.1:2379\nEnvironment=ETCD_CERT_FILE=/etc/ssl/certs/etcd-cert.pem\nEnvironment=ETCD_KEY_FILE=/etc/ssl/certs/etcd-key.pem\nEnvironment=ETCD_TRUSTED_CA_FILE=/etc/ssl/certs/ca-locksmith-cert.pem\nEnvironment=ETCD_CLIENT_CERT_AUTH=true"
        }]
      },
      {
        "name": "locksmithd.service",
        "enable": true,
        "dropins": [{
          "name": "environment.conf",
          "contents": "[Unit]\nAfter=etcd2.service\nRequires=etcd2.service\n[Service]\nEnvironment=LOCKSMITHD_ETCD_CERTFILE=/etc/ssl/certs/locksmith-cert.pem\nEnvironment=LOCKSMITHD_ETCD_KEYFILE=/etc/ssl/certs/locksmith-key.pem\nEnvironment=LOCKSMITHD_ETCD_CAFILE=/etc/ssl/certs/ca-etcd-cert.pem\nEnvironment=LOCKSMITHD_ENDPOINT=https://localhost:2379\nEnvironment=LOCKSMITHD_REBOOT_WINDOW_START=00:00\nEnvironment=LOCKSMITHD_REBOOT_WINDOW_LENGTH=23h59m"
        }]
      }
    ]
  },
  "storage": {
    "files": [
      {
        "filesystem": "root",
        "path": "/etc/coreos/update.conf",
        "contents": { "source": "data:,REBOOT_STRATEGY=etcd-lock%0A" },
        "mode": 420
      },
      {
        "filesystem": "root",
        "path": "/etc/ssl/openssl.cnf",
        "contents": { "source": "data:,%5Breq%5D%0Adistinguished_name=req%0A%5Betcd_ca%5D%0AbasicConstraints=CA:true%0AkeyUsage=keyCertSign,cRLSign%0AsubjectKeyIdentifier=hash%0A%5Betcd_client%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=clientAuth%0AkeyUsage=digitalSignature,keyEncipherment%0A%5Betcd_server%5D%0AbasicConstraints=CA:FALSE%0AextendedKeyUsage=serverAuth%0AkeyUsage=digitalSignature,keyEncipherment%0AsubjectAltName=@sans%0A%5Bsans%5D%0ADNS.1=localhost%0AIP.1=127.0.0.1%0A" },
        "mode": 420
      }
    ]
  }
}`),
		EndVersion: semver.Version{Major: 1662},
		Distros:    []string{"cl"},
	})
}

func locksmithReboot(c cluster.TestCluster) {
	// The machine should be able to reboot without etcd in the default mode
	m := c.Machines()[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	output, err := c.SSH(m, "sudo systemctl stop sshd.socket && locksmithctl send-need-reboot")
	if _, ok := err.(*ssh.ExitMissingError); ok {
		err = nil // A terminated session is perfectly normal during reboot.
	} else if err == io.EOF {
		err = nil // Sometimes copying command output returns EOF here.
	}
	if err != nil {
		c.Fatalf("failed to run \"locksmithctl send-need-reboot\": output: %q status: %q", output, err)
	}

	err = platform.CheckMachine(ctx, m)
	if err != nil {
		c.Fatalf("failed to check rebooted machine: %v", err)
	}

}

func locksmithCluster(c cluster.TestCluster) {
	machs := c.Machines()

	// Wait for all etcd cluster nodes to be ready.
	if err := etcd.GetClusterHealth(c, machs[0], len(machs)); err != nil {
		c.Fatalf("cluster health: %v", err)
	}

	c.MustSSH(machs[0], "locksmithctl status")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	wg := worker.NewWorkerGroup(ctx, len(machs))

	// reboot all the things
	for _, m := range machs {
		worker := func(ctx context.Context) error {
			cmd := "sudo systemctl stop sshd.socket && sudo locksmithctl send-need-reboot"
			output, err := c.SSH(m, cmd)
			if _, ok := err.(*ssh.ExitMissingError); ok {
				err = nil // A terminated session is perfectly normal during reboot.
			} else if err == io.EOF {
				err = nil // Sometimes copying command output returns EOF here.
			}
			if err != nil {
				return fmt.Errorf("failed to run %q: output: %q status: %q", cmd, output, err)
			}

			return platform.CheckMachine(ctx, m)
		}

		if err := wg.Start(worker); err != nil {
			c.Fatal(wg.WaitError(err))
		}
	}

	if err := wg.Wait(); err != nil {
		c.Fatal(err)
	}
}

func locksmithTLS(c cluster.TestCluster) {
	m := c.Machines()[0]
	lCmd := "sudo locksmithctl --endpoint https://localhost:2379 --etcd-cafile /etc/ssl/certs/ca-etcd-cert.pem --etcd-certfile /etc/ssl/certs/locksmith-cert.pem --etcd-keyfile /etc/ssl/certs/locksmith-key.pem "

	// First verify etcd has a valid TLS connection ready
	output, err := c.SSH(m, "openssl s_client -showcerts -verify_return_error -verify_ip 127.0.0.1 -verify_hostname localhost -connect localhost:2379 -cert /etc/ssl/certs/locksmith-cert.pem -key /etc/ssl/certs/locksmith-key.pem 0</dev/null 2>&1")
	if err != nil || !bytes.Contains(output, []byte("Verify return code: 0")) {
		c.Fatalf("openssl s_client: %q: %v", output, err)
	}

	// Also verify locksmithctl understands the TLS connection
	c.MustSSH(m, lCmd+"status")

	// Stop locksmithd
	c.MustSSH(m, "sudo systemctl stop locksmithd.service")

	// Set the lock while locksmithd isn't looking
	c.MustSSH(m, lCmd+"lock")

	// Verify it is locked
	output, err = c.SSH(m, lCmd+"status")
	if err != nil || !bytes.HasPrefix(output, []byte("Available: 0\nMax: 1")) {
		c.Fatalf("locksmithctl status (locked): %q: %v", output, err)
	}

	// Start locksmithd
	c.MustSSH(m, "sudo systemctl start locksmithd.service")

	// Verify it is unlocked (after locksmithd wakes up again)
	checker := func() error {
		output, err := c.SSH(m, lCmd+"status")
		if err != nil || !bytes.HasPrefix(output, []byte("Available: 1\nMax: 1")) {
			return fmt.Errorf("locksmithctl status (unlocked): %q: %v", output, err)
		}
		return nil
	}
	if err := util.Retry(10, 12*time.Second, checker); err != nil {
		c.Fatal(err)
	}
}
