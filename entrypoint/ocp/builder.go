/*
	Main interface into OCP Build targets.

	This supports running via:
	- generic Pod with a Service Account
	- an OpenShift buildConfig

*/

package ocp

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"

	"github.com/coreos/entrypoint/spec"
	rhjobspec "github.com/coreos/entrypoint/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
)

var (
	// cosaSrvDir is where the build directory should be. When the build API
	// defines a contextDir then it will be used. In most cases this should be /srv
	cosaSrvDir = defaultContextDir
)

func init() {
	buildJSONCodec = buildCodecFactory.LegacyCodec(buildapiv1.SchemeGroupVersion)
}

// Builder is defined by envVars set by OpenShift
// See: https://docs.openshift.com/container-platform/4.5/builds/build-strategies.html#builds-strategy-custom-environment-variables_build-strategies
type Builder struct {
	DeveloperMode string `envVar:"COSA_DEVELOPER_MODE"`
	JobSpecURL    string `envVar:"COSA_JOBSPEC_URL"`
	JobSpecRef    string `envVar:"COSA_JOBSPEC_REF"`
	JobSpecFile   string `envVar:"COSA_JOBSPEC_FILE"`
	CosaCmds      string `envVar:"COSA_CMDS"`

	// EnvVars is a listing of specific envVars to set
	EnvVars []string

	// Internal copy of the JobSpec
	JobSpec *spec.JobSpec
}

// NewBuilder reads the environment options and returns a Builder and error.
func NewBuilder() (*Builder, error) {
	v := Builder{}
	rv := reflect.TypeOf(v)
	for i := 0; i < rv.NumField(); i++ {
		tag := rv.Field(i).Tag.Get(ocpStructTag)
		if tag == "" {
			continue
		}
		ev, found := os.LookupEnv(tag)
		if found {
			reflect.ValueOf(&v).Elem().Field(i).SetString(ev)
		}
	}

	// Init the API Client for k8s itself
	// The API client requires that the pod/buildconfig include a service account.
	k8sAPIErr := k8sAPIClient()
	if k8sAPIErr != nil && k8sAPIErr != ErrNotInCluster {
		log.Errorf("Failed to initalized Kubernetes in cluster API client: %v", k8sAPIErr)
		return nil, k8sAPIErr
	}

	// Init the OpenShift Build API Client.
	buildAPIErr := ocpBuildClient()
	if buildAPIErr != nil && buildAPIErr != ErrNoOCPBuildSpec {
		log.Errorf("Failed to initalized the OpenShift Build API Client: %v", buildAPIErr)
		return nil, buildAPIErr
	} else {
		v.EnvVars = append(
			os.Environ(),
			"COSA_SKIP_OVERLAY=skip",
			"FORCE_UNPRIVILEGED=1",
		)
	}
	// Builder requires either a Build API Client or Kuberneres Cluster client.
	if k8sAPIErr != nil && buildAPIErr != nil {
		return nil, ErrInvalidOCPMode
	}

	if _, err := os.Stat(cosaSrvDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("Context dir %q does not exist", cosaSrvDir)
	}
	// Finally check the API client
	return &v, nil
}

// PrepareEnv setups the COSA build environment.
func (o *Builder) PrepareEnv() error {
	if err := os.Chdir(cosaSrvDir); err != nil {
		return fmt.Errorf("Failed to switch to context dir: %s: %v", cosaSrvDir, err)
	}

	// Load secrets directly from the Kubernetes API that are
	// are "well-known" secrets.
	ks, err := kubernetesSecretsSetup(cosaSrvDir)
	if err != nil {
		log.Errorf("Failed to setup Service Account Secrets: %v", err)
	} else {
		log.Infof("Mapped %d secrets from Kubernetes", len(ks))
	}

	// Read setup the secrets locally.
	bs, err := buildSecretsSetup(cosaSrvDir)
	if err != nil {
		log.Errorf("Failed to setup OCP Build Secrets: %v", err)
	} else {
		log.Infof("Mapped %d secrets from Kubernetes", len(bs))
	}

	preCount := len(o.EnvVars)
	o.EnvVars = append(o.EnvVars, bs...)
	o.EnvVars = append(o.EnvVars, ks...)
	addedCount := len(o.EnvVars) - preCount
	log.Infof("Added %d secret envVar mappings", addedCount)

	// Extract any binary sources first. If a binary payload is delivered,
	// then blindly execute any script ending in .cosa.sh
	bin, err := extractInputBinary(cosaSrvDir)
	if err != nil {
		return err
	}

	// Locate the job spec.
	jsF := filepath.Join(cosaSrvDir, rhjobspec.DefaultJobSpecFile)
	js, err := rhjobspec.JobSpecFromFile(jsF)
	if err != nil {
		o.JobSpec = js
	}

	// If there is no binary payload, then init COSA
	// With OCP its either binary _or_ source.
	if !bin {
		if err := cosaInit(o.EnvVars); err != ErrNoSourceInput {
			return err
		}
		log.Info("No source input, relying solely on envVars...this won't end well.")
	}

	return nil
}

// Exec executes the command using the closure for the commands
func (o *Builder) Exec(f func(args []string) error) error {
	curD, _ := os.Getwd()
	defer os.Chdir(curD)
	if err := os.Chdir(cosaSrvDir); err != nil {
		return err
	}

	// By default, binary payloads override all envvars directives.
	// OCP does not allow envVars with binary builds.
	if apiBuild.Spec.Source.Binary != nil {
		scripts, err := filepath.Glob("*.cosa.sh")
		if err != nil {
			return err
		}
		return f(scripts)
	}

	// If there are no binary payloads, write then envVars to
	// to a temporary file, and use that for execution.
	tmpf, err := ioutil.TempFile("", "cosa-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpf.Name())
	content := fmt.Sprintf(strictModeBashTemplate, o.CosaCmds, o.CosaCmds)
	if _, err = tmpf.WriteString(content); err != nil {
		return err
	}
	if err := os.Chmod(tmpf.Name(), 0755); err != nil {
		return err
	}
	return f(
		[]string{tmpf.Name()},
	)
}
