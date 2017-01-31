package bootkube

import (
	"fmt"
	"strings"
	"time"

	"github.com/coreos-inc/pluton"
	"github.com/coreos-inc/pluton/spawn"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/util"
)

func etcdScale(tc cluster.TestCluster) error {
	// create cluster with self-hosted etcd
	c, err := spawn.MakeBootkubeCluster(tc, 1, true)
	if err != nil {
		return err
	}

	// add two master nodes to cluster
	if err := c.AddMasters(2); err != nil {
		return err
	}

	// scale up etcd operator
	if err := resizeSelfHostedEtcd(c, 3); err != nil {
		return err
	}

	// todo check that each pod runs on a different master node
	if err := checkEtcdPodDistribution(c, 3); err != nil {
		return err
	}

	// scale back to 1
	if err := resizeSelfHostedEtcd(c, 1); err != nil {
		return err
	}

	// run an nginx deployment and ping it
	if err := nginxCheck(c); err != nil {
		return fmt.Errorf("nginxCheck: %v", err)
	}
	return nil
}

// resizes self-hosted etcd and checks that the desired number of pods are in a running state
func resizeSelfHostedEtcd(c *pluton.Cluster, size int) error {
	const tprEndpoint = "http://127.0.0.1:8080/apis/coreos.com/v1/namespaces/kube-system/etcdclusters/kube-etcd"

	scaleCmds := []string{
		fmt.Sprintf("curl -H 'Content-Type: application/json' -X GET %v > body.json", tprEndpoint),
		fmt.Sprintf("jq .spec.size=%v < body.json > newbody.json", size),
		fmt.Sprintf("curl -H 'Content-Type: application/json' -X PUT --data @newbody.json %v", tprEndpoint),
	}
	for _, cmd := range scaleCmds {
		_, err := c.Masters[0].SSH(cmd)
		if err != nil {
			return fmt.Errorf("Error in scale up command: %v: %v", cmd, err)
		}
	}

	// check that all 3 pods are running
	podsReady := func() error {
		out, err := c.Kubectl(`get po -l etcd_cluster=kube-etcd -o jsonpath='{.items[*].status.phase}' --namespace=kube-system`)
		if err != nil {
			return err
		}
		phases := strings.Split(out, " ")
		if len(phases) != 3 {
			return fmt.Errorf("Not enough etcd pods got %v: %v", out, phases)
		}
		for _, phase := range phases {
			if phase != "Running" {
				return fmt.Errorf("One or more etcd pods not in a 'Running' phase")
			}
		}
		return nil
	}

	if err := util.Retry(10, 10*time.Second, podsReady); err != nil {
		return err
	}
	return nil
}

// checks that self-hosted etcd pods are scheduled on different master nodes
// when possible
func checkEtcdPodDistribution(c *pluton.Cluster, etcdClusterSize int) error {
	// check that number of unique nodes etcd pods run on is equal to the
	// lesser value betweeen total number of master nodes and total number
	// of etcd pods
	out, err := c.Kubectl(`get po -l etcd_cluster=kube-etcd -o jsonpath='{.items[*].status.hostIP}' --namespace=kube-system`)
	if err != nil {
		return err
	}

	nodeIPs := strings.Split(out, " ")
	nodeSet := map[string]struct{}{}
	for _, node := range nodeIPs {
		nodeSet[node] = struct{}{}
	}

	var expectedUniqueNodes int
	if len(c.Masters) > etcdClusterSize {
		expectedUniqueNodes = etcdClusterSize
	} else {
		expectedUniqueNodes = len(c.Masters)
	}

	if len(nodeSet) != expectedUniqueNodes {
		return fmt.Errorf("self-hosted etcd pods not properly distributed")
	}

	// check that each node in nodeSet is a master node
	masterSet := map[string]struct{}{}
	for _, m := range c.Masters {
		masterSet[m.PrivateIP()] = struct{}{}
	}

	for k, _ := range nodeSet {
		if _, ok := masterSet[k]; !ok {
			return fmt.Errorf("Detected self-hosted etcd pod running on non-master node %v %v", masterSet, nodeSet)
		}
	}

	return nil
}
