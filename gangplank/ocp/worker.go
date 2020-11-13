package ocp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/gangplank/cosa"
	"github.com/coreos/gangplank/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

var (
	// workSpec is a Builder.
	_ = Builder(&workSpec{})
)

// workSpec define job for remote worker to do
// A workSpec is dispatched by a builder and is tightly coupled to
// to the dispatching pod.
type workSpec struct {
	RemoteFiles   []*RemoteFile     `json:"remotefiles"`
	JobSpec       spec.JobSpec      `json:"jobspec"`
	ExecuteStages []string          `json:"executeStages"`
	APIBuild      *buildapiv1.Build `json:"apiBuild"`
	Return        *Return           `json:"return"`
}

const (
	// CosaWorkPodEnvVarName is the envVar used to identify WorkSpec json.
	CosaWorkPodEnvVarName = "COSA_WORK_POD_JSON"
)

// newWorkSpec returns a workspec from the environment
func newWorkSpec(ctx context.Context) (*workSpec, error) {
	w, ok := os.LookupEnv(CosaWorkPodEnvVarName)
	if !ok {
		return nil, ErrNotWorkPod
	}
	r := strings.NewReader(w)
	ws := workSpec{}
	if err := ws.Unmarshal(r); err != nil {
		return nil, err
	}
	if _, err := os.Stat(cosaSrvDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("Context dir %q does not exist", cosaSrvDir)
	}

	if err := os.Chdir(cosaSrvDir); err != nil {
		return nil, fmt.Errorf("Failed to switch to context dir: %s: %v", cosaSrvDir, err)
	}

	return &ws, nil
}

// Unmarshal decodes an io.Reader to a workSpec
func (ws *workSpec) Unmarshal(r io.Reader) error {
	d, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(d, &ws); err != nil {
		return err
	}
	return nil
}

// Marshal returns the JSON of a WorkSpec.
func (ws *workSpec) Marshal() ([]byte, error) {
	return json.Marshal(ws)
}

// Exec executes the work spec tasks.
func (ws *workSpec) Exec(ctx context.Context) error {
	ac, pn, err := k8sInClusterClient()
	if err != nil {
		return fmt.Errorf("failed create a kubernetes client: %w", err)
	}

	// Workers always will use /srv
	if err := os.Chdir(cosaSrvDir); err != nil {
		return fmt.Errorf("unable to switch to %s: %w", cosaSrvDir, err)
	}

	ks, err := kubernetesSecretsSetup(ac, pn, cosaSrvDir)
	if err != nil {
		log.Errorf("Failed to setup Service Account Secrets: %v", err)
	}
	envVars := append(os.Environ(), ks...)

	for _, f := range ws.RemoteFiles {
		destf := filepath.Join(cosaSrvDir, f.Bucket, f.Object)
		destd := filepath.Dir(destf)

		log.Infof("Fetching remote file %s/%s", f.Bucket, f.Object)
		if err := os.MkdirAll(destd, 0755); err != nil {
			return err
		}
		// Decompress the file if needed.
		if f.Compressed {
			if err := f.Extract(ctx, cosaSrvDir); err != nil {
				return fmt.Errorf("failed to decompress from %s/%s: %w", f.Bucket, f.Object, err)
			}
		}
		// Write the file.
		if err := f.WriteToPath(ctx, destf); err != nil {
			return fmt.Errorf("failed to write file from %s/%s: %w", f.Bucket, f.Object, err)
		}
	}

	apiBuild = ws.APIBuild
	if apiBuild != nil {
		bc := apiBuild.Annotations[buildapiv1.BuildConfigAnnotation]
		bn := apiBuild.Annotations[buildapiv1.BuildNumberAnnotation]
		log.Infof("Worker is part of buildconfig.openshift.io/%s-%s", bc, bn)
		if err := cosaInit(); err != nil && err != ErrNoSourceInput {
			return fmt.Errorf("failed to clone recipe: %w", err)
		}
	} else {
		// Emit a warning about running an unbound worker. Unbound workers
		// require something else to create and manage the pod, such as Jenkins.
		log.Warnf("Pod is running as a unbound worker")
	}

	// Ensure on shutdown that we record information that might be
	// needed for prosperity. First, build artifacts are sent off the
	// remote object storage. Then meta.json is written to /dev/console
	// and /dev/termination-log.
	defer func() {
		err := ws.Return.Run(ctx)
		log.WithField("error", err).Info("processed uploads")

		b, _, err := cosa.ReadBuild(cosaSrvDir, "", cosa.BuilderArch())
		if err != nil && b != nil {
			_ = b.WriteMeta(os.Stdout.Name(), false)

			err := b.WriteMeta("/dev/termination-log", false)
			log.WithFields(log.Fields{
				"err":     err,
				"file":    "/dev/termination-log",
				"buildID": b.BuildID,
			}).Info("wrote termination log")
		}
	}()

	var e error = nil

	// Range over the stages and perform the actual work.
	for _, s := range ws.ExecuteStages {
		log.Infof("Executing Stage: %s", s)
		stage, err := ws.JobSpec.GetStage(s)
		log.Infof("Stage commands: %v", stage.Commands)
		if err != nil {
			e = err
		}

		if err := stage.Execute(ctx, &ws.JobSpec, envVars); err != nil {
			log.Errorf("failed stage execution")
			e = err
		}
	}
	log.Infof("Finished execution")
	return e
}

// getEnvVars returns the envVars to be exposed to the worker pod.
// When `newWorkSpec` is called, the envVar will read the embedded string JSON
// and the worker will get its configuration.
func (ws *workSpec) getEnvVars() ([]v1.EnvVar, error) {
	d, err := ws.Marshal()
	if err != nil {
		return nil, err
	}

	return []v1.EnvVar{
		{
			Name:  CosaWorkPodEnvVarName,
			Value: string(d),
		},
	}, nil
}
