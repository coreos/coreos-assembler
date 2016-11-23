package bootkube

import (
	"fmt"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/util"
)

// Run various destruction tests
func bootkubeDestruction(c cluster.TestCluster) error {
	sc, err := MakeSimpleCluster(c)
	if err != nil {
		return err
	}

	if err := masterRestart(sc); err != nil {
		return fmt.Errorf("masterRestart: %s", err)
	}
	return nil
}

// Restart master node and run nginxCheck
func masterRestart(sc *SimpleCluster) error {
	sc.Master.SSH("sudo shutdown now -r") // will always error out

	f := func() error {
		return nodeCheck(sc.Master, sc.Workers)
	}
	if err := util.Retry(10, 20*time.Second, f); err != nil {
		return err
	}

	if err := nginxCheck(sc); err != nil {
		return err
	}
	return nil
}
