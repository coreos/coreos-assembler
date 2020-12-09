// +ci
package ocp

/*
	Since our builds do CI testing in OpenShift for this
	set of tests we have to force not being in the cluster.
*/

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.Debug("CI mode enabled: forcing Kubernetes out-of-cluster errors")
}

func TestNoEnv(t *testing.T) {
	if _, err := newBC(context.Background(), &Cluster{inCluster: false}); err != ErrNoOCPBuildSpec {
		t.Errorf("failed to raise error\n   want: %v\n    got: %v", ErrInvalidOCPMode, err)
	}
}

func TestNoOCP(t *testing.T) {
	newO, err := newBC(context.Background(), &Cluster{inCluster: false})
	if newO != nil {
		t.Errorf("should return nil")
	}
	if err == nil {
		t.Errorf("expected error")
	}
}
