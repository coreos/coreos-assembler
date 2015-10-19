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

package kubernetes

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/kubernetes")

// Start a multi-node cluster from offcial kubernetes 1.0 guides. Once
// up, do a couple basic smoke checks. See:
// http://kubernetes.io/v1.0/docs/getting-started-guides/coreos/coreos_multinode_cluster.html
func MultiNodeSmoke(c platform.TestCluster) error {
	const clusterSize = 3

	// spawn master
	master, err := c.NewMachine(masterConfig)
	if err != nil {
		return err
	}

	// get master private IP and place into nodeConfig
	nodeConfig = strings.Replace(nodeConfig, "<master-private-ip>", master.PrivateIP(), -1)
	var nodeConfigs []string
	for i := 0; i < clusterSize-1; i++ {
		nodeConfigs = append(nodeConfigs, nodeConfig)
	}

	// spawn nodes
	nodes, err := platform.NewMachines(c, nodeConfigs)
	if err != nil {
		return err
	}

	// get kubectl in master
	_, err = master.SSH("wget -q https://storage.googleapis.com/kubernetes-release/release/v1.0.1/bin/linux/amd64/kubectl")
	if err != nil {
		return err
	}
	_, err = master.SSH("chmod +x kubectl")
	if err != nil {
		return err
	}

	// check that all nodes appear in kubectl
	f := func() error {
		if err = nodeCheck(master, nodes); err != nil {
			return err
		}
		return nil
	}
	if err := util.Retry(10, 5*time.Second, f); err != nil {
		return err
	}

	// start nginx pod and curl endpoint
	if err = nginxCheck(master, nodes); err != nil {
		return err
	}

	// http://kubernetes.io/v1.0/docs/user-guide/secrets/ Also, ensures
	// https://github.com/coreos/bugs/issues/447 does not re-occur.
	if err = secretCheck(master, nodes); err != nil {
		return err
	}

	return nil
}

func nodeCheck(master platform.Machine, nodes []platform.Machine) error {
	b, err := master.SSH("./kubectl get nodes")
	if err != nil {
		return err
	}

	// parse kubectl output for IPs
	addrMap := map[string]struct{}{}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		addrMap[strings.SplitN(line, " ", 2)[0]] = struct{}{}
	}

	if len(addrMap) != len(nodes) {
		return fmt.Errorf("cannot detect all nodes in kubectl output \n%v\n%v", addrMap, nodes)
	}
	for _, node := range nodes {
		if _, ok := addrMap[node.PrivateIP()]; !ok {
			return fmt.Errorf("node IP missing from kubectl get nodes")
		}
	}
	return nil
}

func nginxCheck(master platform.Machine, nodes []platform.Machine) error {
	pod := strings.NewReader(nginxPodYAML)
	if err := installFile(pod, master, "./nginx-pod.yaml"); err != nil {
		return err
	}
	if _, err := master.SSH("./kubectl create -f nginx-pod.yaml"); err != nil {
		return err
	}
	// wait for pod status to be 'Running'
	podIsRunning := func() error {
		b, err := master.SSH("./kubectl get pod nginx -o=template -t={{.status.phase}}")
		if err != nil {
			return err
		}
		if !bytes.Equal(b, []byte("Running")) {
			return fmt.Errorf("nginx pod not running: %s", b)
		}
		return nil
	}
	if err := util.Retry(10, 5*time.Second, podIsRunning); err != nil {
		return err
	}

	// delete pod
	_, err := master.SSH("./kubectl delete pods nginx")
	if err != nil {
		return err
	}

	return nil
}

func secretCheck(master platform.Machine, nodes []platform.Machine) error {
	// create yaml files
	secret := strings.NewReader(secretYAML)
	pod := strings.NewReader(secretPodYAML)
	if err := installFile(secret, master, "./secret.yaml"); err != nil {
		return err
	}
	if err := installFile(pod, master, "./secret-pod.yaml"); err != nil {
		return err
	}

	if _, err := master.SSH("./kubectl create -f secret.yaml"); err != nil {
		return err
	}
	_, err := master.SSH("./kubectl describe secret test-secret")
	if err != nil {
		return err
	}

	b, err := master.SSH("./kubectl create -f secret-pod.yaml")
	if err != nil {
		return err
	}
	expectedOutput := "value-1"
	if strings.Contains(strings.TrimSpace(string(b)), expectedOutput) {
		return fmt.Errorf("error detecting secret pod")
	}

	return nil
}

func installFile(in io.Reader, m platform.Machine, to string) error {
	_, err := m.SSH(fmt.Sprintf("sudo mkdir -p %s", filepath.Dir(to)))
	if err != nil {
		return err
	}

	session, err := m.SSHSession()
	if err != nil {
		return fmt.Errorf("Error establishing ssh session: %v", err)
	}
	defer session.Close()

	// write file to fs from stdin
	session.Stdin = in
	err = session.Run(fmt.Sprintf("install -m 0755 /dev/stdin %s", to))
	if err != nil {
		return err
	}
	return nil
}

const (
	secretPodYAML = `apiVersion: v1
kind: Pod
metadata:
  name: secret-test-pod
spec:
  containers:
    - name: test-container
      image: kubernetes/mounttest:0.1
      command: [ "/mt", "--file_content=/etc/secret-volume/data-1" ]
      volumeMounts:
          # name must match the volume name below
          - name: secret-volume
            mountPath: /etc/secret-volume
  volumes:
    - name: secret-volume
      secret:
        secretName: test-secret
  restartPolicy: Never`

	secretYAML = `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
data:
  data-1: dmFsdWUtMQ0K
  data-2: dmFsdWUtMg0KDQo=`

	nginxPodYAML = `apiVersion: v1
kind: Pod
metadata:
  name: nginx
  labels:
    app: nginx
spec:
  containers:
  - name: nginx
    image: nginx
    ports:
    - containerPort: 80`
)
