package ocp

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	ee "github.com/coreos/entrypoint/exec"
	log "github.com/sirupsen/logrus"
)

// cosaInit does the initial COSA setup. To support both pod and buildConfig
// based builds first, check the API client, then check envVars. The use of envVars
// in this case is *safe*; `SOURCE_{URI,REF} == apiBuild.Spec.Source.Git.{URI,REF}`. That
// is, SOURCE_* envVars will always match the apiBuild.Spec.Source.Git.* values.
func cosaInit(envVars []string) error {
	var gitURI, gitRef string
	if apiBuild.Spec.Source.Git != nil {
		gitURI = apiBuild.Spec.Source.Git.URI
		gitRef = apiBuild.Spec.Source.Git.Ref
	} else {
		gitURI, _ = os.LookupEnv("SOURCE_URI")
		gitRef, _ = os.LookupEnv("SOURCE_REF")
	}
	if gitURI == "" {
		log.Info("No Git Source, skipping checkout")
		return ErrNoSourceInput
	}

	args := []string{"init"}
	if gitRef != "" {
		args = append(args, "--force", "--branch", gitRef)
	}
	args = append(args, gitURI)
	log.Infof("running 'git %v'", strings.Join(args, " "))
	cmd := exec.Command("cosa", args...)
	cmd.Env = envVars
	rc, err := ee.RunCmds(cmd)
	if rc != 0 || err != nil {
		log.WithFields(log.Fields{
			"cmd":         args,
			"return code": rc,
			"error":       err,
		}).Error("Failed to checkout respository")
		return errors.New("failed to run cosa init")
	}
	return nil
}
