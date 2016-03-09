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

package kubernetes

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/kubernetes")

// register a separate test for each version tag
var tags = []string{
	"v1.1.7_coreos.2",
	"v1.1.8_coreos.0",
}

func init() {
	for i := range tags {
		// use closure to store a version tag in a Test
		t := tags[i]
		f := func(c platform.TestCluster) error {
			return CoreOSBasic(c, t)
		}

		register.Register(&register.Test{
			Name:        "google.kubernetes.coreosbasic." + tags[i],
			Run:         f,
			ClusterSize: 0,
			Platforms:   []string{"gce", "aws"},
		})
	}
}

// Start a multi-node cluster from offcial coreos guides on manual
// installation. Once up, do a couple basic smoke checks. See:
// https://coreos.com/kubernetes/docs/latest/getting-started.html
func CoreOSBasic(c platform.TestCluster, version string) error {
	// start single-node etcd
	etcdNode, err := c.NewMachine(etcdConfig)
	if err != nil {
		return err
	}

	if err := etcd.GetClusterHealth(etcdNode, 1); err != nil {
		return err
	}

	master, err := c.NewMachine("")
	if err != nil {
		return err
	}

	options := map[string]string{
		"HYPERKUBE_ACI":       "quay.io/coreos/hyperkube",
		"MASTER_HOST":         master.PrivateIP(),
		"ETCD_ENDPOINTS":      fmt.Sprintf("http://%v:2379", etcdNode.PrivateIP()),
		"CONTROLLER_ENDPOINT": fmt.Sprintf("https://%v:443", master.PrivateIP()),
		"K8S_SERVICE_IP":      "10.3.0.1",
		"K8S_VER":             version,
		"KUBELET_PATH":        "/usr/lib/coreos/kubelet-wrapper",
	}

	// generate TLS assets on master
	if err := generateMasterTLSAssets(master, options); err != nil {
		return err
	}

	// create 3 worker nodes
	workerConfigs := []string{"", "", ""}
	workers, err := platform.NewMachines(c, workerConfigs)
	if err != nil {
		return err
	}

	// generate tls assets on workers by transfering ca from master
	if err := generateWorkerTLSAssets(master, workers); err != nil {
		return err
	}

	// configure nodes via generic install scripts
	if err := runInstallScript(master, controllerInstallScript, options); err != nil {
		return fmt.Errorf("Installing controller: %v", err)
	}

	for _, worker := range workers {
		if err := runInstallScript(worker, workerInstallScript, options); err != nil {
			return fmt.Errorf("Installing worker: %v", err)
		}
	}

	// configure kubectl
	if err := configureKubectl(master, master.PrivateIP(), version); err != nil {
		return err
	}

	// check that all nodes appear in kubectl
	f := func() error {
		return nodeCheck(master, workers)
	}
	if err := util.Retry(15, 10*time.Second, f); err != nil {
		return err
	}

	// start nginx pod and curl endpoint
	if err = nginxCheck(master, workers); err != nil {
		return err
	}

	// http://kubernetes.io/v1.0/docs/user-guide/secrets/ Also, ensures
	// https://github.com/coreos/bugs/issues/447 does not re-occur.
	if err = secretCheck(master, workers); err != nil {
		return err
	}

	return nil
}

func generateMasterTLSAssets(master platform.Machine, options map[string]string) error {
	var buffer = new(bytes.Buffer)

	tmpl, err := template.New("masterCNF").Parse(masterCNF)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(buffer, options); err != nil {
		return err
	}

	if err := platform.InstallFile(buffer, master, "/home/core/openssl.cnf"); err != nil {
		return err
	}

	var cmds = []string{
		// gen master assets
		"openssl genrsa -out ca-key.pem 2048",
		`openssl req -x509 -new -nodes -key ca-key.pem -days 10000 -out ca.pem -subj "/CN=kube-ca"`,
		"openssl genrsa -out apiserver-key.pem 2048",
		`openssl req -new -key apiserver-key.pem -out apiserver.csr -subj "/CN=kube-apiserver" -config openssl.cnf`,
		"openssl x509 -req -in apiserver.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out apiserver.pem -days 365 -extensions v3_req -extfile openssl.cnf",

		// gen cluster admin keypair
		"openssl genrsa -out admin-key.pem 2048",
		`openssl req -new -key admin-key.pem -out admin.csr -subj "/CN=kube-admin"`,
		"openssl x509 -req -in admin.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out admin.pem -days 365",

		// move into /etc/kubernetes/ssl
		"sudo mkdir -p /etc/kubernetes/ssl",
		"sudo cp /home/core/ca.pem /etc/kubernetes/ssl/ca.pem",
		"sudo cp /home/core/apiserver.pem /etc/kubernetes/ssl/apiserver.pem",
		"sudo cp /home/core/apiserver-key.pem /etc/kubernetes/ssl/apiserver-key.pem",
		"sudo chmod 600 /etc/kubernetes/ssl/*-key.pem",
		"sudo chown root:root /etc/kubernetes/ssl/*-key.pem",
	}

	for _, cmd := range cmds {
		b, err := master.SSH(cmd)
		if err != nil {
			return fmt.Errorf("Failed on cmd: %s with error: %s and output %s", cmd, err, b)
		}
	}
	return nil
}

func generateWorkerTLSAssets(master platform.Machine, workers []platform.Machine) error {
	for i, worker := range workers {
		// copy tls assets from master to workers
		err := platform.TransferFile(master, "/etc/kubernetes/ssl/ca.pem", worker, "/home/core/ca.pem")
		if err != nil {
			return err
		}
		err = platform.TransferFile(master, "/home/core/ca-key.pem", worker, "/home/core/ca-key.pem")
		if err != nil {
			return err
		}

		// place worker-openssl.cnf on workers
		cnf := strings.Replace(workerCNF, "{{.WORKER_IP}}", worker.PrivateIP(), -1)
		in := strings.NewReader(cnf)
		if err := platform.InstallFile(in, worker, "/home/core/worker-openssl.cnf"); err != nil {
			return err
		}

		// gen certs
		workerFQDN := fmt.Sprintf("kube-worker-%v", i)
		cmds := []string{
			fmt.Sprintf("openssl genrsa -out worker-key.pem 2048"),
			fmt.Sprintf(`openssl req -new -key worker-key.pem -out %v-worker.csr -subj "/CN=%v" -config worker-openssl.cnf`, workerFQDN, workerFQDN),
			fmt.Sprintf(`openssl x509 -req -in %v-worker.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out worker.pem -days 365 -extensions v3_req -extfile worker-openssl.cnf`, workerFQDN),

			// move into /etc/kubernetes/ssl
			"sudo mkdir -p /etc/kubernetes/ssl",
			"sudo chmod 600 /home/core/*-key.pem",
			"sudo chown root:root /home/core/*-key.pem",
			"sudo cp /home/core/worker.pem /etc/kubernetes/ssl/worker.pem",
			"sudo cp /home/core/worker-key.pem /etc/kubernetes/ssl/worker-key.pem",
			"sudo cp /home/core/ca.pem /etc/kubernetes/ssl/ca.pem",
		}

		for _, cmd := range cmds {
			b, err := worker.SSH(cmd)
			if err != nil {
				return fmt.Errorf("Failed on cmd: %s with error: %s and output %s", cmd, err, b)
			}
		}
	}
	return nil
}

// https://coreos.com/kubernetes/docs/latest/configure-kubectl.html
func configureKubectl(m platform.Machine, server string, version string) error {
	// ignore suffix like '-coreos.1' on version to grab upstream
	semverPrefix := regexp.MustCompile(`^v[\d]+\.[\d]+\.[\d]+`)
	version = semverPrefix.FindString(version)
	if version == "" {
		return fmt.Errorf("Failure to parse kubectl version")
	}

	var (
		ca        = "/home/core/ca.pem"
		adminKey  = "/home/core/admin-key.pem"
		adminCert = "/home/core/admin.pem"
		kubeURL   = fmt.Sprintf("https://storage.googleapis.com/kubernetes-release/release/%v/bin/linux/amd64/kubectl", version)
	)

	if _, err := m.SSH("wget -q " + kubeURL); err != nil {
		return err
	}
	if _, err := m.SSH("chmod +x ./kubectl"); err != nil {
		return err
	}

	// cmds to configure kubectl
	cmds := []string{
		fmt.Sprintf("./kubectl config set-cluster default-cluster --server=https://%v --certificate-authority=%v", server, ca),
		fmt.Sprintf("./kubectl config set-credentials default-admin --certificate-authority=%v --client-key=%v --client-certificate=%v", ca, adminKey, adminCert),
		"./kubectl config set-context default-system --cluster=default-cluster --user=default-admin",
		"./kubectl config use-context default-system",
	}
	for _, cmd := range cmds {
		b, err := m.SSH(cmd)
		if err != nil {
			return fmt.Errorf("Failed on cmd: %s with error: %s and output %s", cmd, err, b)
		}
	}
	return nil
}

// Run and configure the coreos-kubernetes generic install scripts.
func runInstallScript(m platform.Machine, script string, options map[string]string) error {
	// attempt to directly use kubelet if kubelet-wrapper not on disk
	// on-disk wrapper should exist as of release 962.0.0
	if _, err := m.SSH("sudo stat /usr/lib/coreos/kubelet-wrapper"); err != nil {
		plog.Errorf("on-disk kubelet-wrapper not found, using CoreOS built-in kubelet")
		options["KUBELET_PATH"] = "/usr/bin/kubelet"
	}

	var buffer = new(bytes.Buffer)

	tmpl, err := template.New("installScript").Parse(script)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(buffer, options); err != nil {
		return err
	}

	if err := platform.InstallFile(buffer, m, "/home/core/install.sh"); err != nil {
		return err
	}

	// use client to collect stderr
	client, err := m.SSHClient()
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stderr := bytes.NewBuffer(nil)
	session.Stderr = stderr

	err = session.Start("sudo /home/core/install.sh")
	if err != nil {
		return err
	}

	// timeout script to prevent it looping forever
	errc := make(chan error)
	go func() {
		errc <- session.Wait()
	}()
	select {
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("%s", stderr)
		}
	case <-time.After(time.Minute * 5):
		return fmt.Errorf("Timed out waiting for install script to finish.")
	}

	return nil
}

const (
	etcdConfig = `#cloud-config

coreos:
  etcd2:
    name: $name
    advertise-client-urls: http://$public_ipv4:2379
    listen-client-urls: http://0.0.0.0:2379,http://0.0.0.0:4001
  units:
    - name: etcd2.service
      command: start`

	masterCNF = `[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
[alt_names]
DNS.1 = kubernetes
DNS.2 = kubernetes.default
IP.1 = {{.K8S_SERVICE_IP}}
IP.2 = {{.MASTER_HOST}}`

	workerCNF = `[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
[alt_names]
IP.1 = {{.WORKER_IP}}`
)
