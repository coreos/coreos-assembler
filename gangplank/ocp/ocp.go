package ocp

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	// These are used to parse the OpenShift API
	buildScheme       = runtime.NewScheme()
	buildCodecFactory = serializer.NewCodecFactory(buildScheme)
	buildJSONCodec    runtime.Codec

	// API Client for OpenShift builds.
	apiBuild *buildapiv1.Build
)

func init() {
	buildJSONCodec = buildCodecFactory.LegacyCodec(buildapiv1.SchemeGroupVersion)
}

// ocpBuildClient initalizes the OpenShift Build Client API.
func ocpBuildClient() error {
	// Use the OpenShift API to parse the build meta-data.
	envVarBuild, okay := os.LookupEnv("BUILD")
	if !okay {
		return ErrNoOCPBuildSpec
	}
	cfg := &buildapiv1.Build{}
	obj, _, err := buildJSONCodec.Decode([]byte(envVarBuild), nil, cfg)
	if err != nil {
		return ErrNoOCPBuildSpec
	}
	ok := false
	apiBuild, ok = obj.(*buildapiv1.Build)
	if !ok {
		return ErrNoOCPBuildSpec
	}

	// Check to make sure that this is actually on an OpenShift build node.
	strategy := apiBuild.Spec.Strategy
	if strategy.Type != "" && strategy.Type != "Custom" {
		return fmt.Errorf("unsupported build strategy")
	}
	log.Info("Executing OpenShift custom strategy builder.")

	// Check to make sure that we have a valid contextDir
	// Almost _always_ this should be in /srv for COSA.
	cDir := apiBuild.Spec.Source.ContextDir
	if cDir != "" && cDir != "/" {
		log.Infof("Using %s as working directory.", cDir)
		cosaSrvDir = cDir
	}
	return nil
}

// getPushTagless returns the registry, and push path
// i.e registry.svc:5000/image/bar:tag returns "registry.svc:5000" and "image/bar"
func getPushTagless(s string) (string, string) {
	pushParts := strings.Split(s, "/")
	// split the name and tag
	splitLast := strings.Split(pushParts[len(pushParts)-1], ":")
	// strip off the tag, and replace it with just the name
	pushPathParts := strings.Split(strings.TrimRight(s, strings.Join(splitLast, ":"))+splitLast[0], "/")
	return pushParts[0], strings.Join(pushPathParts[1:], "/")
}

// uploadCustomBuildContainer implements the custom build strategy optional step to report the results
// to the registry as an OCI image. uploadCustomBuildContainer must be called from a worker pod.
func uploadCustomBuildContainer(ctx ClusterContext, apiBuild *buildapiv1.Build, buildID string) error {
	if apiBuild == nil || apiBuild.Spec.Strategy.CustomStrategy == nil || apiBuild.Spec.Output.To == nil {
		log.Debug("Build is not running as a custom build strategy or output name is not defined")
		return nil
	}

	// Setup the environment for commands
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("unable to determine my username: %v", err)
	}
	authPath := filepath.Join("run", "containers", u.Uid, "auth.json")
	baseEnv := append(
		os.Environ(),
		"FORCE_UNPRIVILEGED=1",
		fmt.Sprintf("REGISTRY_AUTH_FILE=%s", authPath),
		// Tell the tools where to find the home directory
		fmt.Sprintf("HOME=%s", cosaSrvDir),
	)

	// envVar is set during Pod creation by workSpec.getEnvVars()
	disableTLSVerification, _ := os.LookupEnv("DISABLE_TLS_VERIFICATION")

	registry, registryPath := getPushTagless(apiBuild.Spec.Output.To.Name)
	pushPath := fmt.Sprintf("%s/%s", registry, registryPath)

	l := log.WithFields(
		log.Fields{
			"authfile":   authPath,
			"final push": apiBuild.Spec.Output.To.Name,
			"push path":  pushPath,
			"registry":   registry,
		})
	l.Info("Pushing to remote registry")

	if token, err := ioutil.ReadFile(serviceAccountTokenFile); err == nil {
		// Default to using the service account token to login to the registry
		// This method is the most reliable for OCP3 and while its a hack, it ensures that the
		// the auth is located where the tooling expects it to be.
		l.Debug("Using token to access registry")
		loginCmd := []string{"buildah", "login"}
		if disableTLSVerification == "1" {
			loginCmd = append(loginCmd, "--tls-verify=false")
		}
		loginCmd = append(loginCmd, "-u", "serviceaccount", "-p", string(token), registry)

		cmd := exec.CommandContext(ctx, loginCmd[0], loginCmd[1:]...)
		cmd.Env = baseEnv
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to login into registry: %v", err)
		}
	} else if apiBuild.Spec.Output.PushSecret != nil && apiBuild.Spec.Output.PushSecret.Name != "" {
		// otherwise locate the build push secret. On OCP3 this is not reliable
		l.Debug("Using named secret for registry access")
		ac, ns, err := GetClient(ctx)
		if err != nil {
			return fmt.Errorf("unable to fetch push secret")
		}
		secret, err := ac.CoreV1().Secrets(ns).Get(apiBuild.Spec.Output.PushSecret.Name, v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to query for secret %s: %v", apiBuild.Spec.Output.PushSecret.Name, err)
		}
		if secret == nil {
			return fmt.Errorf("secret is empty")
		}

		// Populate the docker config.
		for k, v := range secret.Data {
			if k != ".dockercfg" {
				continue
			}
			log.WithFields(log.Fields{
				"key":         string(k),
				"secret name": secret.Name,
			}).Info("Writing push secret")

			authDir := filepath.Dir(authPath)
			if err := os.MkdirAll(authDir, 0755); err != nil {
				return fmt.Errorf("failed to create docker configuration directory")
			}

			if err := ioutil.WriteFile(authPath, v, 0444); err != nil {
				return fmt.Errorf("failed writing secret %s:%s", secret.Name, k)
			}
		}
	}

	// pushArgs invokes cosa upload code which creates a named tag
	pushArgs := []string{
		"/usr/bin/coreos-assembler", "upload-oscontainer",
		fmt.Sprintf("--name=%s", pushPath),
	}
	// copy the pushed image to the expected tag
	copyArgs := []string{
		"skopeo", "copy",
		fmt.Sprintf("docker://%s:%s", pushPath, buildID),
		fmt.Sprintf("docker://%s", apiBuild.Spec.Output.To.Name),
	}
	if disableTLSVerification == "1" {
		copyArgs = append(copyArgs, "--src-tls-verify=false", "--dest-tls-verify=false")
	}

	for _, args := range [][]string{pushArgs, copyArgs} {
		l.WithField("cmd", args).Debug("Calling eternal tool ")
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
