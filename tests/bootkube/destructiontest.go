package bootkube

import (
	"fmt"

	"github.com/coreos-inc/pluton"
	"github.com/coreos-inc/pluton/spawn"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
)

// Run various destruction tests
func bootkubeDestruction(c cluster.TestCluster) error {
	bc, err := spawn.MakeBootkubeCluster(c)
	if err != nil {
		return err
	}

	if err := masterRestart(bc); err != nil {
		return fmt.Errorf("masterRestart: %s", err)
	}

	// TODO: add more destructive tests. Also test that workloads started
	// before any destruction test are still working after the destruction
	// test rather then creating a new workload after the destructive test.

	return nil
}

// Restart master node and run nginxCheck
func masterRestart(c *pluton.Cluster) error {
	if err := platform.Reboot(c.Masters[0]); err != nil {
		return err
	}

	if err := c.NodeCheck(25); err != nil {
		return fmt.Errorf("nodeCheck: %s", err)
	}

	if err := nginxCheck(c); err != nil {
		return fmt.Errorf("nginxCheck: %s", err)
	}
	return nil
}
