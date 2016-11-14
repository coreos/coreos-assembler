package bootkube

import (
	"github.com/coreos/mantle/kola/cluster"
)

func bootkubeSmoke(c cluster.TestCluster) error {
	// This should will not return until cluster is ready
	_, err := MakeSimpleCluster(c)
	if err != nil {
		return err
	}
	// TODO add basic smoke tests here. Some examples: schedule an nginx
	// pod and ping it, test that a pod network is correctly running. The
	// setup function should be blocking on all the basic components being
	// running.
	return nil
}
