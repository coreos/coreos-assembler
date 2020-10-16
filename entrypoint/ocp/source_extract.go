package ocp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// extractInputBinary processes the provided input stream as directed by BinaryBuildSource
// into dir. OpenShift sends binary builds over stdin. To make our life easier,
// use the OpenShift API to process the input. Returns true if this a binary
// input was receieved.
func extractInputBinary(dir string) (bool, error) {
	os.MkdirAll(dir, 0777)
	source := apiBuild.Spec.Source.Binary
	if source == nil {
		log.Debug("No binary payload found")
		return false, nil
	}

	// If stdin is a file, then write it out to the same name
	// as send from the OCP binary.
	var path string
	if len(source.AsFile) > 0 {
		log.Infof("Receiving source from STDIN as file %s", source.AsFile)
		path = filepath.Join(dir, source.AsFile)

		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return false, err
		}
		defer f.Close()
		n, err := io.Copy(f, os.Stdin)
		if err != nil {
			return false, err
		}
		log.Infof("Received %d bytes into %s", n, path)
		return true, nil
	}

	// Otherwise, the file is an archive. The OpenShift build client
	// uses bsdtart since it supports zip, tar, compressed tar.
	log.Info("Receiving binary source from STDIN as archive ...")
	args := []string{"-x", "-v", "-o", "-m", "-f", "-", "-C", dir}
	cmd := exec.Command("bsdtar", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	log.Infof("Extracting...\n%s", string(out))
	if err != nil {
		return false, fmt.Errorf("unable to extract binary build input, must be a zip, tar, or gzipped tar, or specified as a file: %v", err)
	}

	return true, nil
}
