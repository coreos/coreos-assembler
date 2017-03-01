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
	"fmt"
	"strings"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/pluton/spawn"
	"github.com/coreos/mantle/util"
)

// TODO: Test that workloads started before any destruction tests are still
// working after the destruction tests rather then creating a new workload after
// the destructive test.

// Restart master node and check that cluster is still functional
func rebootMaster(tc cluster.TestCluster) error {
	c, err := spawn.MakeBootkubeCluster(tc, 1, false)
	if err != nil {
		return err
	}

	// reboot and wait for api to come up 3 times to avoid false positives
	for i := 0; i < 3; i++ {
		if err := c.Masters[0].Reboot(); err != nil {
			return err
		}

		// TODO(pb) find a way to globally disable selinux in kola
		_, err := c.Masters[0].SSH("sudo setenforce 0")
		if err != nil {
			return fmt.Errorf("turning off selinux failed: %v", err)
		}

		if err := c.NodeCheck(25); err != nil {
			return fmt.Errorf("nodeCheck: %s", err)
		}
	}

	if err := nginxCheck(c); err != nil {
		return fmt.Errorf("nginxCheck: %s", err)
	}
	return nil
}

// Delete api-server pod and wait for it to recover
func deleteAPIServer(tc cluster.TestCluster) error {
	c, err := spawn.MakeBootkubeCluster(tc, 1, false)
	if err != nil {
		return err
	}

	out, err := c.Kubectl("get pods -l k8s-app=kube-apiserver -o=jsonpath={.items[*].metadata.name} --namespace=kube-system")
	if err != nil {
		return err
	}

	apipods := strings.Split(strings.TrimSpace(out), " ")
	if len(apipods) < 1 {
		return fmt.Errorf("Failed detect any apiserver pods for deletion")
	}

	for _, pod := range apipods {
		_, err := c.Kubectl("delete pod " + pod + " --namespace=kube-system")
		if err != nil {
			return fmt.Errorf("Unable to delete apiserver pod: %s", err)
		}
	}

	// wait until kubectl errors out to know that apiserver terminated
	f := func() error {
		out, err := c.Kubectl("get pods --all-namespaces")
		if err == nil {
			return fmt.Errorf(out)
		}
		return nil
	}
	if err := util.Retry(40, 1*time.Second, f); err != nil {
		return fmt.Errorf("apiserver never terminated: %s", err)
	}

	// wait for apiserver to return
	if err := c.NodeCheck(25); err != nil {
		return fmt.Errorf("nodeCheck: %s", err)
	}

	if err := nginxCheck(c); err != nil {
		return fmt.Errorf("nginxCheck: %s", err)
	}

	return nil
}
