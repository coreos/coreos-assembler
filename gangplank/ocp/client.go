package ocp

import (
	"context"
	"os"

	log "github.com/sirupsen/logrus"
)

// Builder implements the Build
type Builder interface {
	Exec(ctx context.Context) error
}

var (
	// cosaSrvDir is where the build directory should be. When the build API
	// defines a contextDir then it will be used. In most cases this should be /srv
	cosaSrvDir = defaultContextDir
)

func init() {
	if _, ok := os.LookupEnv(localPodEnvVar); ok {
		log.Infof("EnvVar %s defined, using local pod mode", localPodEnvVar)
		forceNotInCluster = true
	}
}

// NewBuilder returns a Builder. NewBuilder determines what
// "Builder" to return.
func NewBuilder(ctx context.Context) (Builder, error) {
	ws, err := newWorkSpec(ctx)
	if err == nil {
		return ws, nil
	}
	bc, err := newBC()
	if err == nil {
		return bc, nil
	}
	return nil, ErrNoWorkFound
}
