package spec

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	ee "github.com/coreos/entrypoint/exec"
	log "github.com/sirupsen/logrus"
)

// DefaultJobSpecFile is the default JobSpecFile name.
const DefaultJobSpecFile = "jobspec.yaml"

// cloneJobSpec clones the a jobspec from git repo.
func cloneJobSpec(url, ref, specFile string) (*JobSpec, error) {
	// Fetch the remote jobspec
	if url == "" {
		log.Debug("jobpsec url is not defined, skipping")
		return nil, nil
	}

	tmpd, err := ioutil.TempDir("", "*-entry")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpd)

	// Clone the JobSpec Repo
	jsD := filepath.Join(tmpd, "jobspec")
	args := []string{"clone"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, jsD)
	cmd := exec.Command("git", args...)
	rc, err := ee.RunCmds(cmd)
	if rc != 0 {
		if err == nil {
			err = errors.New("non-zero exit from command")
		}
		return nil, err
	}

	jsF := specFile
	if jsF == "" {
		jsF = DefaultJobSpecFile
	}
	ns, err := JobSpecFromFile(filepath.Join(jsD, jsF))
	if err != nil {
		return nil, err
	}
	log.Infof("found jobspec for %q", ns.Job.BuildName)
	return ns, nil
}
