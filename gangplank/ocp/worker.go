package ocp

import (
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

// workSpec is a Builder.
var _ Builder = &workSpec{}

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

// CosaWorkPodEnvVarName is the envVar used to identify WorkSpec json.
const CosaWorkPodEnvVarName = "COSA_WORK_POD_JSON"

// newWorkSpec returns a workspec from the environment
func newWorkSpec(ctx ClusterContext) (*workSpec, error) {
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

	log.Info("Running as a worker pod")
	return &ws, nil
}

// Unmarshal decodes an io.Reader to a workSpec.
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
func (ws *workSpec) Exec(ctx ClusterContext) error {
	apiBuild = ws.APIBuild
	envVars := os.Environ()

	// Check stdin for binary input.
	inF, err := recieveInputBinary()
	if err == nil && inF != "" {
		log.WithField("file", inF).Info("Worker recieved binary input")

		f, err := os.Open(inF)
		if err != nil {
			return err
		}
		if err := decompress(f, cosaSrvDir); err != nil {
			return err
		}

		// Add select paths to the path for developer overrides
		for i, ev := range envVars {
			kvs := strings.Split(ev, "=")
			if kvs[0] == "PATH" {
				envVars[i] = fmt.Sprintf("%s/bin:%s/cosa/src/:%s", cosaSrvDir, cosaSrvDir, kvs[1])
			}
		}

	}

	// Workers always will use /srv. The shell/Python code of COSA expects
	// /srv to be on its own volume.
	if err := os.Chdir(cosaSrvDir); err != nil {
		return fmt.Errorf("unable to switch to %s: %w", cosaSrvDir, err)
	}

	// Setup the incluster client
	ac, pn, err := k8sInClusterClient()
	if err == ErrNotInCluster {
		log.Info("Worker is out-of-clstuer, no secrets will be available")
	} else if err != nil {
		return fmt.Errorf("failed create a kubernetes client: %w", err)
	}

	// Only setup secrets for in-cluster use
	if ac != nil {
		ks, err := kubernetesSecretsSetup(ac, pn, cosaSrvDir)
		if err != nil {
			log.Errorf("Failed to setup Service Account Secrets: %v", err)
		}
		envVars = append(envVars, ks...)
	}

	// Identify the buildConfig that launched this instance.
	if apiBuild != nil {
		bc := apiBuild.Annotations[buildapiv1.BuildConfigAnnotation]
		bn := apiBuild.Annotations[buildapiv1.BuildNumberAnnotation]
		log.Infof("Worker is part of buildconfig.openshift.io/%s-%s", bc, bn)
		if err := cosaInit(); err != nil && err != ErrNoSourceInput {
			return fmt.Errorf("failed to clone recipe: %w", err)
		}
	} else {
		// Inform that Gangplank is running as an unbound worker. Unbound workers
		// require something else to create and manage the pod, such as Jenkins.
		log.Infof("Pod is running as a unbound worker")
	}

	// Fetch the remote files and write them to the local path.
	for _, f := range ws.RemoteFiles {
		destf := filepath.Join(cosaSrvDir, f.Bucket, f.Object)
		log.Infof("Fetching remote file %s/%s", f.Bucket, f.Object)
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

	// Populate `src/config/<name.repo>`
	for _, r := range ws.JobSpec.Recipe.Repos {
		path, err := r.Writer(filepath.Join("src", "config"))
		if err != nil {
			return fmt.Errorf("failed to write remote repo: %v", err)
		}
		log.WithFields(log.Fields{
			"path": path,
			"url":  r.URL,
		}).Info("Wrote repo definition from url")
	}

	// Ensure on shutdown that we record information that might be
	// needed for prosperity. First, build artifacts are sent off the
	// remote object storage. Then meta.json is written to /dev/console
	// and /dev/termination-log.
	defer func() {
		if ws.Return != nil {
			err := ws.Return.Run(ctx, ws)
			log.WithError(err).Info("Processed Uploads")
		}

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

	// Expose the jobspec and meta.json (if its available) for templating.
	mBuild, _, _ := cosa.ReadBuild(cosaSrvDir, "", cosa.BuilderArch())
	if mBuild == nil {
		mBuild = new(cosa.Build)
	}
	rd := &spec.RenderData{
		JobSpec: &ws.JobSpec,
		Meta:    mBuild,
	}

	// Ensure a latest symlink to the build exists
	if mBuild.BuildID != "" {
		if err := func() error {
			pwd, _ := os.Getwd()
			defer os.Chdir(pwd) //nolint

			if err := os.Chdir(filepath.Join(cosaSrvDir, "builds")); err != nil {
				return fmt.Errorf("unable to change to builds dir: %v", err)
			}

			// create a relative symlink in the builds dir
			latestLink := filepath.Join("latest")
			latestTarget := filepath.Join(mBuild.BuildID)
			if _, err := os.Lstat(latestLink); os.IsNotExist(err) {
				if err := os.Symlink(latestTarget, latestLink); err != nil {
					return fmt.Errorf("unable to create latest symlink from %s to %s", latestTarget, latestLink)
				}
			}
			return nil
		}(); err != nil {
			return err
		}
	}

	// Range over the stages and perform the actual work.
	for _, s := range ws.ExecuteStages {
		stage, err := ws.JobSpec.GetStage(s)
		l := log.WithFields(log.Fields{
			"stage id":           s,
			"build artifacts":    stage.BuildArtifacts,
			"required artifacts": stage.RequireArtifacts,
			"optional artifacts": stage.RequestArtifacts,
			"commands":           stage.Commands,
		})
		l.Info("Executing Stage")

		if err != nil {
			l.WithError(err).Info("Error fetching stage")
			return err
		}

		if err := stage.Execute(ctx, rd, envVars); err != nil {
			l.WithError(err).Error("failed stage execution")
			return err
		}

		if stage.ReturnCache {
			l.WithField("tarball", cacheTarballName).Infof("Sending %s back as a tarball", cosaSrvCache)
			if err := returnPathTarBall(ctx, cacheBucket, cacheTarballName, cosaSrvCache, ws.Return); err != nil {
				return err
			}
		}

		if stage.ReturnCacheRepo {
			l.WithField("tarball", cacheRepoTarballName).Infof("Sending %s back as a tarball", cosaSrvTmpRepo)
			if err := returnPathTarBall(ctx, cacheBucket, cacheRepoTarballName, cosaSrvTmpRepo, ws.Return); err != nil {
				return err
			}
		}

	}
	log.Infof("Finished execution")
	return nil
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
