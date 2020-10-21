// +ci
package ocp

/*
	Since our builds do CI testing in OpenShift for this
	set of tests we have to force not being in the cluster.
*/

import (
	"testing"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.Debug("CI mode enabled: forcing Kubernetes out-of-cluster errors")
	forceNotInCluster = true
}

func TestNoEnv(t *testing.T) {
	_, err := NewBuilder(testCtx)
	if err != ErrInvalidOCPMode {
		t.Errorf("failed to raise: %v", ErrInvalidOCPMode)
	}
}

func TestNoOCP(t *testing.T) {
	newO, err := NewBuilder(testCtx)
	if newO != nil {
		t.Errorf("should return nil")
	}
	if err == nil {
		t.Errorf("expected error")
	}
}
