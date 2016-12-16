package bootkube

import (
	"github.com/coreos-inc/pluton/spawn"
	"github.com/coreos-inc/pluton/upstream"

	"github.com/coreos/mantle/kola/cluster"
)

func conformanceBootkube(c cluster.TestCluster) error {
	pc, err := spawn.MakeBootkubeCluster(c, 3)
	if err != nil {
		return err
	}

	// cmdline options
	var (
		repo    = c.Options["ConformanceRepo"]
		version = c.Options["ConformanceVersion"]
	)

	return upstream.RunConformanceTests(pc, repo, version)
}
