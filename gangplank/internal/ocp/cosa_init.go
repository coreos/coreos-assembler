package ocp

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/coreos/gangplank/internal/spec"
	log "github.com/sirupsen/logrus"
)

const (
	envVarSourceURI = "SOURCE_URI"
	envVarSourceRef = "SOURCE_REF"
)

// cosaInit does the initial COSA setup. To support both pod and buildConfig
// based builds, first check the API client, then check envVars. The use of envVars
// in this case is *safe*; `SOURCE_{URI,REF} == apiBuild.Spec.Source.Git.{URI,REF}`. That
// is, SOURCE_* envVars will always match the apiBuild.Spec.Source.Git.* values.
func cosaInit(js spec.JobSpec) error {
	_ = os.Chdir(cosaSrvDir)
	var gitURI, gitRef, gitCommit string
	if js.Recipe.GitURL != "" {
		gitURI = js.Recipe.GitURL
		gitRef = js.Recipe.GitRef
		gitCommit = js.Recipe.GitCommit
	} else if apiBuild != nil && apiBuild.Spec.Source.Git != nil {
		gitURI = apiBuild.Spec.Source.Git.URI
		gitRef = apiBuild.Spec.Source.Git.Ref
	} else {
		gitURI, _ = os.LookupEnv(envVarSourceURI)
		gitRef, _ = os.LookupEnv(envVarSourceRef)
	}
	if gitURI == "" {
		log.Info("No Git Source, skipping checkout")
		return ErrNoSourceInput
	}

	args := []string{"cosa", "init"}
	if gitRef != "" {
		args = append(args, "--force", "--branch", gitRef)
	}
	if gitCommit != "" {
		args = append(args, "--commit", gitCommit)
	}
	args = append(args, gitURI)
	log.Infof("running '%v'", strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		out, _ := cmd.CombinedOutput()
		log.WithFields(log.Fields{
			"cmd":   args,
			"error": err,
			"out":   string(out),
		}).Error("Failed to checkout respository")
		return errors.New("failed to run cosa init")
	}
	return nil
}
