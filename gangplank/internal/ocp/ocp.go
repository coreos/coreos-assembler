package ocp

import (
	"fmt"
	"os"
	"strings"

	"github.com/coreos/coreos-assembler-schema/cosa"
	"github.com/coreos/gangplank/internal/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
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
// The token used is associated with the service account with the worker pod.
func uploadCustomBuildContainer(ctx ClusterContext, tlsVerify *bool, apiBuild *buildapiv1.Build, build *cosa.Build) error {
	if apiBuild == nil || apiBuild.Spec.Strategy.CustomStrategy == nil || apiBuild.Spec.Output.To == nil {
		log.Debug("Build is not running as a custom build strategy or output name is not defined")
		return nil
	}

	err := pushOstreeToRegistry(ctx,
		&spec.Registry{
			URL:        apiBuild.Spec.Output.To.Name,
			SecretType: spec.PushSecretTypeToken,
			TLSVerify:  tlsVerify,
		},
		build)

	if err != nil {
		log.WithError(err).Error("failed to push to remote registry")
	}

	return err
}
