package spec

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// DefaultJobSpecFile is the default JobSpecFile name.
const DefaultJobSpecFile = "jobspec.yaml"

// JobSpecFromRepo clones a git repo and returns the jobspec and error.
func JobSpecFromRepo(url, ref, specFile string) (JobSpec, error) {
	// Fetch the remote jobspec
	var j JobSpec
	if url == "" {
		log.Debug("jobpsec url is not defined, skipping")
		return j, nil
	}

	tmpd, err := ioutil.TempDir("", "arrmatey")
	if err != nil {
		return j, err
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
	if err := cmd.Run(); err != nil {
		return j, err
	}

	jsF := specFile
	if jsF == "" {
		jsF = DefaultJobSpecFile
	}
	ns, err := JobSpecFromFile(filepath.Join(jsD, jsF))
	if err != nil {
		return j, err
	}
	log.Infof("found jobspec for %q", ns.Job.BuildName)

	return ns, nil
}
