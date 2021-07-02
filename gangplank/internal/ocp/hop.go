package ocp

/*
	Hop Pod's allow for Gangplank to create a pod in a remote cluster.

	The goal in a hop pod is two fold:
	- CI environments like Prow (where Gangplank is run with insufficent perms)
	- Developers who want to run Gangplank remotely
*/

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/gangplank/internal/spec"
	"github.com/opencontainers/runc/libcontainer/user"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// hopPod describes a remote pod for running Gangplank in a
// cluster remotely.
type hopPod struct {
	clusterCtx ClusterContext
	js         *spec.JobSpec

	image          string
	ns             string
	serviceAccount string
}

// GetClusterCtx returns the cluster context of a hopPod
func (h *hopPod) GetClusterCtx() ClusterContext {
	return h.clusterCtx
}

// hopPod implements the CosaPodder interface.
var _ CosaPodder = &hopPod{}

// NewHopPod returns a PodBuilder.
func NewHopPod(ctx ClusterContext, image, serviceAccount, workDir string, js *spec.JobSpec) CosaPodder {
	cosaSrvDir = workDir
	return &hopPod{
		clusterCtx:     ctx,
		image:          image,
		serviceAccount: serviceAccount,
		js:             js,
	}
}

// Exec Gangplank locally through a remote/hop pod that runs
// Gangplank in a cluster.
func (h *hopPod) WorkerRunner(term termChan, _ []v1.EnvVar) error {
	if h.image == "" {
		return errors.New("image must not be empty")
	}
	if h.serviceAccount == "" {
		return errors.New("service account must not be empty")
	}
	return clusterRunner(term, h, nil)
}

// getSpec createa a very generic pod that can run on any Cluster.
// The pod will mimic a build api pod.
func (h *hopPod) getPodSpec([]v1.EnvVar) (*v1.Pod, error) {
	log.Debug("Generating hop podspec")

	u, _ := user.CurrentUser()
	podName := fmt.Sprintf("%s-hop-%d",
		u.Name, time.Now().UTC().Unix(),
	)
	log.Infof("Creating pod %s", podName)

	buf := bytes.Buffer{}
	if err := h.js.WriteYAML(&buf); err != nil {
		return nil, err
	}

	script := `#!/bin/bash -xe
find /run/secrets
cat > jobspec.yaml <<EOM
${JOBSPEC}
EOM
cat jobspec.yaml
gangplank pod --spec jobspec.yaml || { sleep 30; exit $?; }
`
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: h.ns,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "worker",
					Image: h.image,
					Command: []string{
						"/usr/bin/dumb-init",
					},
					Args: []string{
						"/bin/bash", "-xc",
						script,
					},
					Env: []v1.EnvVar{
						{
							Name:  "JOBSPEC",
							Value: buf.String(),
						},
					},
					WorkingDir:   "/srv",
					Stdin:        true,
					StdinOnce:    true,
					VolumeMounts: volumeMounts,
					TTY:          true,
				},
			},
			ActiveDeadlineSeconds:         ptrInt(1800),
			AutomountServiceAccountToken:  ptrBool(true),
			RestartPolicy:                 v1.RestartPolicyNever,
			ServiceAccountName:            h.serviceAccount,
			TerminationGracePeriodSeconds: ptrInt(60),
			Volumes:                       volumes,
		},
	}

	return pod, nil
}
