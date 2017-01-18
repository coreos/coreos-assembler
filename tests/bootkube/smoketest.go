package bootkube

import (
	"bytes"
	"fmt"
	"time"

	"github.com/coreos-inc/pluton"
	"github.com/coreos-inc/pluton/spawn"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/util"
)

func bootkubeSmoke(c cluster.TestCluster) error {
	// This should not return until cluster is ready
	bc, err := spawn.MakeBootkubeCluster(c, 1, false)
	if err != nil {
		return err
	}

	// run an nginx deployment and ping it
	if err := nginxCheck(bc); err != nil {
		return fmt.Errorf("nginxCheck: %s", err)
	}
	// TODO add more basic or regression tests here
	return nil
}

func bootkubeSmokeEtcd(c cluster.TestCluster) error {
	// This should not return until cluster is ready
	bc, err := spawn.MakeBootkubeCluster(c, 1, true)
	if err != nil {
		return err
	}

	// run an nginx deployment and ping it
	if err := nginxCheck(bc); err != nil {
		return fmt.Errorf("nginxCheck: %s", err)
	}
	return nil
}

func nginxCheck(c *pluton.Cluster) error {
	// start nginx deployment
	_, err := c.Kubectl("run my-nginx --image=nginx --replicas=2 --port=80")
	if err != nil {
		return err
	}

	// expose nginx
	_, err = c.Kubectl("expose deployment my-nginx --port=80 --type=LoadBalancer")
	if err != nil {
		return err
	}
	serviceIP, err := c.Kubectl("get service my-nginx --template={{.spec.clusterIP}}")
	if err != nil {
		return err
	}

	// curl for welcome message
	nginxRunning := func() error {
		out, err := c.Masters[0].SSH("curl " + serviceIP + ":80")
		if err != nil || !bytes.Contains(out, []byte("Welcome to nginx!")) {
			return fmt.Errorf("unable to reach nginx: %s", out)
		}
		return nil
	}
	if err := util.Retry(15, 10*time.Second, nginxRunning); err != nil {
		return err
	}

	// delete pod
	_, err = c.Kubectl("delete deployment my-nginx")
	if err != nil {
		return err
	}

	return nil
}
