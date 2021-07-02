package main

/*
	Gangway is the Gangplank worker.
*/

import (
	"context"

	"github.com/coreos/gangplank/internal/ocp"
	log "github.com/sirupsen/logrus"
)

func main() {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	defer ctx.Done()

	cluster := ocp.NewCluster(true)
	clusterCtx := ocp.NewClusterContext(ctx, cluster)

	b, err := ocp.NewBuilder(clusterCtx)
	if err != nil {
		log.Fatal("Failed to find the build environment.")
	}

	if err := b.Exec(clusterCtx); err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("Failed to prepare environment.")
	}

}
