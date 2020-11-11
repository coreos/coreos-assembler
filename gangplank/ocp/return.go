package ocp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/entrypoint/cosa"
	log "github.com/sirupsen/logrus"
)

var (
	// Return is a Returner
	_ = Returner(&Return{})
)

// Return describes the location of where to send results.
type Return struct {
	Minio     *minioServer `json:"remote"`
	Bucket    string       `json:"bucket"`
	Overwrite bool         `json:"overwrite"`

	// ArtifactTypes will return only artifacts that known and defined
	// For example []string{"aws","azure"}
	ArtifactTypes []string `json:"artifacts"`

	// Return all files found in the builds directory
	All bool `json:"all"`
}

// Returner sends the results to the ReportServer
type Returner interface {
	Run(ctx context.Context) error
}

// Run executes the report by walking the build path.
func (r *Return) Run(ctx context.Context) error {
	b, path, err := cosa.ReadBuild(cosaSrvDir, "", cosa.BuilderArch())
	if err != nil {
		return err
	}
	if b == nil {
		return nil
	}
	upload := make(map[string]string)

	// Capture /srv/builds/builds.json
	bJSONpath := filepath.Join(cosaSrvDir, "builds", cosa.CosaBuildsJSON)
	if _, err := os.Stat(bJSONpath); err == nil {
		upload["builds.json"] = bJSONpath
	}

	// Get all the builds files
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to read build dir %s: %w", path, err)
	}

	// Get the builds
	keyPath := filepath.Join(b.BuildID, cosa.BuilderArch())
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		upKey := filepath.Join(keyPath, f.Name())
		srcPath := filepath.Join(path, f.Name())

		// Check if this a meta type
		if strings.HasSuffix(f.Name(), ".json") {
			upload[upKey] = srcPath
		}
		// Check if this a known build artifact
		if _, ok := b.IsArtifact(filepath.Base(f.Name())); ok {
			upload[upKey] = srcPath
		}
	}

	// Now any kola logs.
	tmpFiles, _ := ioutil.ReadDir(filepath.Join(cosaSrvDir, "tmp"))
	for _, f := range tmpFiles {
		upKey := filepath.Join(keyPath, "logs", f.Name())
		srcPath := filepath.Join(path, f.Name())
		if strings.Contains(f.Name(), "kola") && strings.HasSuffix(f.Name(), "tar.xz") {
			upload[upKey] = srcPath
		}
	}

	var e error = nil
	for k, v := range upload {
		l := log.WithFields(log.Fields{
			"host":          r.Minio.Host,
			"file":          v,
			"remote/bucket": r.Bucket,
			"remote/key":    k,
			"overwrite":     r.Overwrite,
		})

		l.Info("uploading")
		if err := r.Minio.putter(ctx, r.Bucket, k, v, r.Overwrite); err != nil {
			l.WithField("err", err).Error("failed upload, tainting build")
			e = fmt.Errorf("upload failed with at least one error: %w", err)
		}
	}
	return e
}
