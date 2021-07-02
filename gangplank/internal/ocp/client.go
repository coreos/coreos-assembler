package ocp

import (
	"os"

	log "github.com/sirupsen/logrus"
)

// Builder implements the Build
type Builder interface {
	Exec(ctx ClusterContext) error
}

// cosaSrvDir is where the build directory should be. When the build API
// defines a contextDir then it will be used. In most cases this should be /srv
var cosaSrvDir = defaultContextDir

// NewBuilder returns a Builder. NewBuilder determines what
// "Builder" to return by first trying Worker and then an OpenShift builder.
func NewBuilder(ctx ClusterContext) (Builder, error) {
	inCluster := true
	if _, ok := os.LookupEnv(localPodEnvVar); ok {
		log.Infof("EnvVar %s defined, using local pod mode", localPodEnvVar)
		inCluster = false
	}

	ws, err := newWorkSpec(ctx)
	if err == nil {
		return ws, nil
	}
	bc, err := newBC(ctx, &Cluster{inCluster: inCluster})
	if err == nil {
		return bc, nil
	}
	return nil, ErrNoWorkFound
}
