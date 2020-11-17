package ocp

import (
	"fmt"
	"os"

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
		log.Infof("Using %s as in-pod working directory.", cDir)
		cosaSrvDir = cDir
	}

	return nil
}
