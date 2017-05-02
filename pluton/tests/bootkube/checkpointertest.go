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

package bootkube

import (
	"bytes"
	"fmt"
	"time"

	"github.com/coreos/mantle/pluton"
	"github.com/coreos/mantle/util"
)

// 1. Schedule a pod checkpointer on worker node.
// 2. Schedule a test pod on worker node.
// 3. Reboot the worker without starting the kubelet.
// 4. Delete the checkpointer on API server.
// 5. Reboot the master without starting the kubelet.
// 6. Start the worker kubelet, verify the checkpointer and the pod is still running as a static pod.
// 7. Start the master kubelet, verify both the checkpointer and the pod are removed.
func unscheduleCheckpointer(c *pluton.Cluster) {
	// Run the pod checkpointer on worker nodes as well.
	_, err := c.Kubectl(`patch daemonset pod-checkpointer --type=json -p='[{"op": "replace", "path": "/spec/template/spec/nodeSelector", "value": {}}]' -n kube-system`)
	if err != nil {
		c.Fatalf("unable to patch daemonset: %v", err)
	}

	// Create test pod.
	_, err = c.Kubectl(`create -f - <<EOF
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: nginx-daemonset
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: nginx
      annotations:
        checkpointer.alpha.coreos.com/checkpoint: "true"
    spec:
      hostNetwork: true
      containers:
        - name: nginx
          image: nginx
EOF`)
	if err != nil {
		c.Fatalf("unable to create the checkpoint parent: %v", err)
	}

	// Verify the checkpoints are created.
	if err := verifyCheckpoint(c, "kube-system", "pod-checkpointer", true, true); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	if err := verifyCheckpoint(c, "default", "nginx-daemonset", true, false); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}

	// Reboot the worker.
	if err := c.Workers[0].Reboot(); err != nil {
		c.Fatalf("unable to reboot worker: %v", err)
	}

	// disable selinux
	_, err = c.Workers[0].SSH("sudo setenforce 0")
	if err != nil {
		c.Fatalf("unable to disable selinux: %v", err)
	}

	// Delete the pod checkpointer on the worker node by update the daemonset.
	_, err = c.Kubectl(`patch daemonset pod-checkpointer --type=json -p='[{"op": "replace", "path": "/spec/template/spec/nodeSelector", "value": {"node-role.kubernetes.io/master":""}}]' -n kube-system`)

	// Reboot the master.
	if err := c.Masters[0].Reboot(); err != nil {
		c.Fatalf("unable to reboot master: %v", err)
	}

	// disable selinux
	_, err = c.Masters[0].SSH("sudo setenforce 0")
	if err != nil {
		c.Fatalf("unable to disable selinux: %v", err)
	}

	// Start the worker kubelet.
	out, err := c.Workers[0].SSH("sudo systemctl start kubelet")
	if err != nil {
		c.Fatalf("unable to start worker kubelet: %s", out)
	}

	// Verify that the checkpoints are still running.
	if err := verifyPod(c, "pod-checkpointer", true); err != nil {
		c.Fatalf("verifyPod: %s", err)

	}
	if err := verifyPod(c, "nginx-daemonset", true); err != nil {
		c.Fatalf("verifyPod: %s", err)

	}

	// Start the master kubelet.
	out, err = c.Masters[0].SSH("sudo systemctl start kubelet")
	if err != nil {
		c.Fatalf("unable to start master kubelet: %s", out)
	}

	// Verify that the pod-checkpointer is cleaned up but the daemonset is still running.
	if err := verifyPod(c, "pod-checkpointer", false); err != nil {
		c.Fatalf("verifyPod: %s", err)
	}
	if err := verifyPod(c, "nginx-daemonset", true); err != nil {
		c.Fatalf("verifyPod: %s", err)
	}
	if err := verifyCheckpoint(c, "kube-system", "pod-checkpointer", false, false); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	if err := verifyCheckpoint(c, "default", "nginx-daemonset", false, false); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	return
}

// 1. Schedule a pod checkpointer on worker node.
// 2. Schedule a test pod on worker node.
// 3. Reboot the worker without starting the kubelet.
// 4. Delete the test pod on API server.
// 5. Reboot the master without starting the kubelet.
// 6. Start the worker kubelet, verify the checkpointer and the pod is still running as a static pod.
// 7. Start the master kubelet, verify the test pod are removed, but not the checkpointer.
func unscheduleCheckpointParent(c *pluton.Cluster) {
	// Run the pod checkpointer on worker nodes as well.
	_, err := c.Kubectl(`patch daemonset pod-checkpointer --type=json -p='[{"op": "replace", "path": "/spec/template/spec/nodeSelector", "value": {}}]' -n kube-system`)
	if err != nil {
		c.Fatalf("unable to patch daemonset: %v", err)
	}

	// Create test pod.
	_, err = c.Kubectl(`create -f - <<EOF
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: nginx-daemonset
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: nginx
      annotations:
        checkpointer.alpha.coreos.com/checkpoint: "true"
    spec:
      hostNetwork: true
      containers:
        - name: nginx
          image: nginx
EOF`)
	if err != nil {
		c.Fatalf("unable to create the checkpoint parent: %v", err)
	}

	// Verify the checkpoints are created.
	if err := verifyCheckpoint(c, "kube-system", "pod-checkpointer", true, true); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	if err := verifyCheckpoint(c, "default", "nginx-daemonset", true, false); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}

	// Reboot the worker.
	if err := c.Workers[0].Reboot(); err != nil {
		c.Fatalf("unable to reboot worker: %v", err)
	}

	// disable selinux
	_, err = c.Workers[0].SSH("sudo setenforce 0")
	if err != nil {
		c.Fatalf("unable to disable selinux: %v", err)
	}

	// Delete test pod.
	_, err = c.Kubectl(`patch daemonset nginx-daemonset --type=json -p='[{"op": "replace", "path": "/spec/template/spec/nodeSelector", "value": {"node-role.kubernetes.io/master":""}}]'`)
	if err != nil {
		c.Fatalf("unable to patch daemonset: %v", err)
	}

	// Reboot the master.
	if err := c.Masters[0].Reboot(); err != nil {
		c.Fatalf("unable to reboot master: %v", err)
	}

	// disable selinux
	_, err = c.Masters[0].SSH("sudo setenforce 0")
	if err != nil {
		c.Fatalf("unable to disable selinux: %v", err)
	}

	// Start the worker kubelet.
	out, err := c.Workers[0].SSH("sudo systemctl start kubelet")
	if err != nil {
		c.Fatalf("unable to start worker kubelet: %s", out)
	}

	// Verify that the checkpoints are running.
	if err := verifyPod(c, "pod-checkpointer", true); err != nil {
		c.Fatalf("verifyPod: %s", err)

	}
	if err := verifyPod(c, "nginx-daemonset", true); err != nil {
		c.Fatalf("verifyPod: %s", err)

	}

	// Start the master kubelet.
	out, err = c.Masters[0].SSH("sudo systemctl start kubelet")
	if err != nil {
		c.Fatalf("unable to start master kubelet: %s", out)
	}

	// Verify that checkpoint is cleaned up and not running, but the pod checkpointer should still be running.
	if err := verifyPod(c, "pod-checkpointer", true); err != nil {
		c.Fatalf("verifyPod: %s", err)
	}
	if err := verifyPod(c, "nginx-daemonset", false); err != nil {
		c.Fatalf("verifyPod: %s", err)
	}
	if err := verifyCheckpoint(c, "kube-system", "pod-checkpointer", true, true); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	if err := verifyCheckpoint(c, "default", "nginx-daemonset", false, false); err != nil {
		c.Fatalf("verifyCheckpoint: %s", err)
	}
	return
}

func verifyCheckpoint(c *pluton.Cluster, namespace, daemonsetName string, shouldExist, shouldBeActive bool) error {
	checkpointed := func() error {
		dirs := []string{
			"/etc/kubernetes/inactive-manifests/",
			"/etc/kubernetes/checkpoint-secrets/" + namespace,
			// TODO(yifan): Add configmap.
		}

		if shouldBeActive {
			dirs = append(dirs, "/etc/kubernetes/manifests")
		}

		for _, dir := range dirs {
			out, err := c.Workers[0].SSH("sudo ls " + dir)
			if err != nil {
				return fmt.Errorf("unable to ls %q, error: %v, output: %q", dir, err, out)
			}

			if shouldExist && !bytes.Contains(out, []byte(daemonsetName)) {
				return fmt.Errorf("unable to find checkpoint %q in %q: error: %v, output: %q", daemonsetName, dir, err, out)
			}
			if !shouldExist && bytes.Contains(out, []byte(daemonsetName)) {
				return fmt.Errorf("should not find checkpoint %q in %q, error: %v, output: %q", daemonsetName, dir, err, out)
			}
		}

		// Check active checkpoints.
		dir := "/etc/kubernetes/manifests"
		out, err := c.Workers[0].SSH("sudo ls " + dir)
		if err != nil {
			return fmt.Errorf("unable to ls %q, error: %v, output: %q", dir, err, out)
		}
		if shouldBeActive && !bytes.Contains(out, []byte(daemonsetName)) {
			return fmt.Errorf("unable to find checkpoint %q in %q: error: %v, output: %q", daemonsetName, dir, err, out)
		}
		if !shouldBeActive && bytes.Contains(out, []byte(daemonsetName)) {
			return fmt.Errorf("should not find checkpoint %q in %q, error: %v, output: %q", daemonsetName, dir, err, out)
		}

		return nil
	}
	return util.Retry(20, 10*time.Second, checkpointed)
}

func verifyPod(c *pluton.Cluster, daemonsetName string, shouldRun bool) error {
	checkpointsRunning := func() error {
		out, err := c.Workers[0].SSH("docker ps")
		if err != nil {
			return fmt.Errorf("unable to docker ps, error: %v, output: %q", err, out)
		}

		if shouldRun && !bytes.Contains(out, []byte(daemonsetName)) {
			return fmt.Errorf("unable to find running checkpoints %q, error: %v, output: %q", daemonsetName, err, out)
		}
		if !shouldRun && bytes.Contains(out, []byte(daemonsetName)) {
			return fmt.Errorf("should not find running checkpoints %q, error: %v, output: %q", daemonsetName, err, out)
		}
		return nil
	}
	return util.Retry(20, 10*time.Second, checkpointsRunning)
}
