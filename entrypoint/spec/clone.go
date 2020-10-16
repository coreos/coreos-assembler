package spec

import (
	"errors"
	"io/ioutil"
	"os"
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
	gitCmd := []string{"git", "clone"}
	if ref != "" {
		gitCmd = append(gitCmd, "--branch", ref)
	}
	gitCmd = append(gitCmd, url, jsD)
	rc, err := ee.RunCmds(gitCmd)
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
