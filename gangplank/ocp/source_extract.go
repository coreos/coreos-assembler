package ocp

import (
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

const (
	// sourceBin stores binary input
	sourceBin = "source.bin"

	// sourceSubPath is used when extracting binary inputs
	sourceSubPath = "source"
)

// extractInputBinary processes the provided input stream as directed by BinaryBuildSource
// into dir. OpenShift sends binary builds over stdin. To make our life easier,
// use the OpenShift API to process the input. Returns the name of the file
// written.
func recieveInputBinary() (string, error) {
	srcd := filepath.Join(cosaSrvDir, sourceSubPath)
	if err := os.MkdirAll(srcd, 0777); err != nil {
		return "", err
	}
	_ = os.Chdir(srcd)
	defer func() { _ = os.Chdir(cosaSrvDir) }()

	source := apiBuild.Spec.Source.Binary
	if source == nil {
		log.Debug("No binary payload found")
		return "", nil
	}

	// If stdin is a file, then write it out to the same name
	// as send from the OCP binary
	path := filepath.Join(srcd, sourceBin)
	if len(source.AsFile) > 0 {
		path = filepath.Join(srcd, source.AsFile)
	}
	log.Infof("Receiving source from STDIN as file %s", path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	n, err := io.Copy(f, os.Stdin)
	if err != nil {
		return "", err
	}
	log.Infof("Received %d bytes into %s", n, path)
	return path, nil
}
