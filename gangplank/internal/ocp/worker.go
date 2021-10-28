package ocp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler-schema/cosa"
	"github.com/coreos/gangplank/internal/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workSpec is a Builder.
var _ Builder = &workSpec{}

// workerBuild Dir is hard coded. Workers always read builds relative to their
// local paths and assume the build location is on /srv
var workerBuildDir string = filepath.Join("/srv", "builds")

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
		return nil, fmt.Errorf("context dir %q does not exist", cosaSrvDir)
	}

	if err := os.Chdir(cosaSrvDir); err != nil {
		return nil, fmt.Errorf("failed to switch to context dir: %s: %v", cosaSrvDir, err)
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
		ks, err := kubernetesSecretsSetup(ctx, ac, pn, cosaSrvDir)
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
		if err := cosaInit(ws.JobSpec); err != nil && err != ErrNoSourceInput {
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

		b, _, err := cosa.ReadBuild(workerBuildDir, "", cosa.BuilderArch())
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
	mBuild, _, _ := cosa.ReadBuild(workerBuildDir, "", cosa.BuilderArch())
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
			"return files":       stage.ReturnFiles,
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

		next, _, _ := cosa.ReadBuild(workerBuildDir, "", cosa.BuilderArch())
		if next != nil && next.BuildArtifacts != nil && (mBuild.BuildArtifacts == nil || mBuild.BuildArtifacts.Ostree.Sha256 != next.BuildArtifacts.Ostree.Sha256) {
			log.Debug("Stage produced a new OStree")

			// push for other defined registries
			for _, v := range ws.JobSpec.PublishOscontainer.Registries {
				if err := pushOstreeToRegistry(ctx, &v, next); err != nil {
					log.WithError(err).Warningf("Push to registry %s failed", v.URL)
					return err
				}
			}

			// push for custom build strategy
			if err := uploadCustomBuildContainer(ctx, ws.JobSpec.PublishOscontainer.BuildStrategyTLSVerify, ws.APIBuild, next); err != nil {
				log.WithError(err).Warning("Push to BuildSpec registry failed")
				return err
			}
		}

		if stage.ReturnCache {
			l.WithField("tarball", cacheTarballName).Infof("Sending %s back as a tarball", cosaSrvCache)
			if err := uploadPathAsTarBall(ctx, cacheBucket, cacheTarballName, cosaSrvCache, "", true, ws.Return); err != nil {
				return err
			}
		}

		if stage.ReturnCacheRepo {
			l.WithField("tarball", cacheRepoTarballName).Infof("Sending %s back as a tarball", cosaSrvTmpRepo)
			if err := uploadPathAsTarBall(ctx, cacheBucket, cacheRepoTarballName, cosaSrvTmpRepo, "", true, ws.Return); err != nil {
				return err
			}
		}
		if len(stage.ReturnFiles) != 0 {
			l.WithField("files", stage.ReturnFiles).Infof("Sending requested files back to remote")
			if err := uploadReturnFiles(ctx, cacheBucket, stage.ReturnFiles, ws.Return); err != nil {
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

	evars := []v1.EnvVar{
		{
			Name:  CosaWorkPodEnvVarName,
			Value: string(d),
		},
		{
			Name:  "XDG_RUNTIME_DIR",
			Value: cosaSrvDir,
		},
		{
			Name:  "COSA_FORCE_ARCH",
			Value: cosa.BuilderArch(),
		},
	}
	return evars, nil
}

// pushOstreetoRegistry pushes the OStree to the defined registry location.
func pushOstreeToRegistry(ctx ClusterContext, push *spec.Registry, build *cosa.Build) error {
	if push == nil {
		return errors.New("unable to push to nil registry")
	}
	if build == nil {
		return errors.New("unable to push to registry: cosa build is nil")
	}

	// TODO: move this to a common validator
	if push.URL == "" {
		return errors.New("push registry URL is emtpy")
	}

	cluster, _ := GetCluster(ctx)

	registry, registryPath := getPushTagless(push.URL)
	pushPath := fmt.Sprintf("%s/%s", registry, registryPath)

	authPath := filepath.Join(cosaSrvDir, ".docker", "config.json")
	authDir := filepath.Dir(authPath)
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("failed to directory path for push secret")
	}

	switch v := strings.ToLower(string(push.SecretType)); v {
	case spec.PushSecretTypeInline:
		if err := ioutil.WriteFile(authPath, []byte(push.Secret), 0644); err != nil {
			return fmt.Errorf("failed to write the inline secret to auth.json: %v", err)
		}
	case spec.PushSecretTypeCluster:
		if !cluster.inCluster {
			return errors.New("cluster secrets pushes are invalid out-of-cluster")
		}
		if err := writeDockerSecret(ctx, push.Secret, authPath); err != nil {
			return fmt.Errorf("failed to locate the secret %s: %v", push.Secret, err)
		}
	case spec.PushSecretTypeToken:
		if err := tokenRegistryLogin(ctx, push.TLSVerify, registry); err != nil {
			return fmt.Errorf("failed to login into registry: %v", err)
		}
		// container XDG_RUNTIME_DIR is set to cosaSrvDir
		authPath = filepath.Join(cosaSrvDir, "containers", "auth.json")
	default:
		return fmt.Errorf("secret type %s is unknown for push registries", push.SecretType)
	}

	defer func() {
		// Remove any logins that could interfere later with subsequent pushes.
		_ = os.RemoveAll(filepath.Join(cosaSrvDir, "containers"))
		_ = os.RemoveAll(filepath.Join(cosaSrvDir, ".docker"))
	}()

	baseEnv := append(
		os.Environ(),
		"FORCE_UNPRIVILEGED=1",
		fmt.Sprintf("REGISTRY_AUTH_FILE=%s", authPath),
		// Tell the tools where to find the home directory
		fmt.Sprintf("HOME=%s", cosaSrvDir),
	)

	tlsVerify := true
	if push.TLSVerify != nil && !*push.TLSVerify {
		tlsVerify = false
	}

	l := log.WithFields(
		log.Fields{
			"auth json":        authPath,
			"final push":       push.URL,
			"push path":        pushPath,
			"registry":         registry,
			"tls verification": tlsVerify,
			"push definition":  push,
		})
	l.Info("Pushing to remote registry")

	// pushArgs invokes cosa upload code which creates a named tag
	pushArgs := []string{
		"/usr/bin/coreos-assembler", "upload-oscontainer",
		fmt.Sprintf("--name=%s", pushPath),
	}
	// copy the pushed image to the expected tag
	copyArgs := []string{
		"skopeo", "copy",
		fmt.Sprintf("docker://%s:%s", pushPath, build.BuildID),
		fmt.Sprintf("docker://%s", push.URL),
	}

	if !tlsVerify {
		log.Warnf("TLS Verification has been disable for push to %s", push.URL)
		copyArgs = append(copyArgs, "--src-tls-verify=false", "--dest-tls-verify=false")
		baseEnv = append(baseEnv, "DISABLE_TLS_VERIFICATION=1")
	}

	for _, args := range [][]string{pushArgs, copyArgs} {
		l.WithField("cmd", args).Debug("Calling external tool ")
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Env = baseEnv
		if err := cmd.Run(); err != nil {
			return errors.New("upload to registry failed")
		}
	}
	return nil
}

// writeDockerSecret writes the .dockerCfg or .dockerconfig to the correct path.
// It accepts the cluster context, the name of the secret and the location to write to.
func writeDockerSecret(ctx ClusterContext, clusterSecretName, authPath string) error {
	ac, ns, err := GetClient(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch cluster client: %v", err)
	}
	secret, err := ac.CoreV1().Secrets(ns).Get(ctx, clusterSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to query for secret %s: %v", apiBuild.Spec.Output.PushSecret.Name, err)
	}
	if secret == nil {
		return fmt.Errorf("secret is empty")
	}

	var key string
	switch secret.Type {
	case v1.SecretTypeDockerConfigJson:
		key = v1.DockerConfigJsonKey
	case v1.SecretTypeDockercfg:
		key = v1.DockerConfigKey
	case v1.SecretTypeOpaque:
		if _, ok := secret.Data["docker.json"]; ok {
			key = "docker.json"
		} else if _, ok := secret.Data["docker.cfg"]; ok {
			key = "docker.cfg"
		}
	default:
		return fmt.Errorf("writeDockerSecret is not supported for secret type %s", secret.Type)
	}

	data, ok := secret.Data[key]
	if !ok {
		return fmt.Errorf("secret %s of type %s is malformed: missing %s", secret.Name, secret.Type, key)
	}

	log.WithFields(log.Fields{
		"local path": authPath,
		"type":       string(secret.Type),
		"secret key": key,
		"name":       secret.Name,
	}).Info("Writing push secret")

	if err := ioutil.WriteFile(authPath, data, 0444); err != nil {
		return fmt.Errorf("failed writing secret %s to %s", secret.Name, authPath)
	}
	return nil
}
