package ocp

/*
	Handle Secrets provided by OpenShfit.
*/

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// copySecret is a hack. OpenShift presents secrets with 0400.
// Since syscall.SetUid is blocked, we need to use sudo read the file.
func copySecret(inDir, name, from string, ret *[]string) error {
	sDir := filepath.Join(inDir, "secrets", name)
	if err := os.MkdirAll(sDir, 0755); err != nil {
		return err
	}

	baseName := filepath.Base(from)
	to := filepath.Join(sDir, baseName)
	if _, err := os.Stat(to); err == nil {
		log.Infof("Already handled secret %s", to)
		return nil
	}

	cmd := exec.Command("sudo", "cat", from)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	// OCP secrets are limited at 1Mb....so this "safe-ish"
	if err := ioutil.WriteFile(to, out, 0555); err != nil {
		return err
	}
	log.Infof("Created secret: %s", to)

	// If an envVar SECRET_MAP_FILE_$SECRET matches split the value
	// and set left side to the right.
	// i.e. SECRET_MAP_FILE_MY_SECRET with "AWS_SECRET=CONFIG",
	// then "AWS_SECRET" would be set the <PATH>/CONFIG
	e := fmt.Sprintf("%s%s", cosaSecretMapFile, name)
	ev, found := os.LookupEnv(e)
	if found {
		v := strings.Split(ev, "=")
		if len(v) != 2 {
			return fmt.Errorf("%s envVar should be in format 'K=V', not %s", e, ev)
		}
		if v[1] == baseName {
			*ret = append(*ret, fmt.Sprintf("%s=%s", v[0], to))
		}
	}

	return nil
}

// buildSecretsSetup copies the pod mapped secrets so that the build-user is
// able to read them. Secrets on 3.x clusters are mapped to 0400 and owned by root.
// To allow the non-privilaged build user access, they need to be copied before
// /usr/lib/coreos-assembler sudo's to the builder user.
func buildSecretsSetup(contextDir string) ([]string, error) {
	var ret []string
	if apiBuild.Spec.Source.Secrets == nil {
		return ret, nil
	}
	secrets := apiBuild.Spec.Source.Secrets
	if len(secrets) == 0 {
		return ret, nil
	}

	log.Infof("Build has defined %d secrets.", len(secrets))
	for _, s := range secrets {
		log.WithFields(log.Fields{
			"secret": s.Secret.Name,
		}).Debug("Secret found")

		fs, err := filepath.Glob(fmt.Sprintf("%s/%s/*/*", ocpSecretDir, s.Secret.Name))
		if err != nil {
			log.Errorf("Failed to get file listing: %v", err)
			return ret, err
		}

		for _, f := range fs {
			log.Infof("Processing secret: %s at %s", s.Secret.Name, f)
			if err := copySecret(contextDir, s.Secret.Name, f, &ret); err != nil {
				return ret, err
			}
		}
	}

	return ret, nil
}
