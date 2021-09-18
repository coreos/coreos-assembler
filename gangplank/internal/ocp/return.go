package ocp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler-schema/cosa"
	log "github.com/sirupsen/logrus"
)

// Return is a Returner
var _ Returner = &Return{}

// Return describes the location of where to send results.
type Return struct {
	Minio     *minioServer `json:"remote"`
	Bucket    string       `json:"bucket"`
	KeyPrefix string       `json:"key_prefix"`
	Overwrite bool         `json:"overwrite"`

	// ArtifactTypes will return only artifacts that known and defined
	// For example []string{"aws","azure"}
	ArtifactTypes []string `json:"artifacts"`

	// Return all files found in the builds directory
	All bool `json:"all"`
}

// Returner sends the results to the ReportServer
type Returner interface {
	Run(ctx context.Context, ws *workSpec) error
}

// Run executes the report by walking the build path.
func (r *Return) Run(ctx context.Context, ws *workSpec) error {
	if r.Minio == nil {
		return nil
	}
	baseBuildDir := filepath.Join(cosaSrvDir, "builds")
	b, path, err := cosa.ReadBuild(baseBuildDir, "", cosa.BuilderArch())
	if err == cosa.ErrNoBuildsFound {
		// If there are no builds, ignore
		return nil
	} else if err != nil {
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
		if isKnownBuildMeta(f.Name()) {
			upload[upKey] = srcPath
		}

		// Check if this a known build artifact that was not fetched
		if _, ok := b.IsArtifact(filepath.Base(f.Name())); ok {
			fetched := false
			for _, v := range ws.RemoteFiles {
				if upKey == v.Object {
					log.WithField("local path", f.Name()).Debug("skipping upload of file that was fetched")
					fetched = true
					continue
				}
			}
			if !fetched {
				upload[upKey] = srcPath
			}
		}
	}

	// Now any kola logs.
	tmpDir := filepath.Join(cosaSrvDir, "tmp")
	tmpFiles, _ := ioutil.ReadDir(tmpDir)
	for _, f := range tmpFiles {
		upKey := filepath.Join(keyPath, "logs", f.Name())
		srcPath := filepath.Join(tmpDir, f.Name())
		if strings.Contains(f.Name(), "kola") && strings.HasSuffix(f.Name(), "tar.xz") {
			upload[upKey] = srcPath
		}
	}

	var e error = nil
	for k, v := range upload {
		if r.KeyPrefix != "" {
			k = filepath.Join(r.KeyPrefix, k)
		}

		l := log.WithFields(log.Fields{
			"host":          r.Minio.Host,
			"file":          v,
			"remote/bucket": r.Bucket,
			"remote/key":    k,
		})

		// only overwrite meta if newer.
		if newer, err := r.Minio.isLocalNewer(r.Bucket, k, v); err != nil && newer {
			base := filepath.Base(v)
			if isKnownBuildMeta(base) {
				if newer {
					l.WithField("overrite", true).Info("overwrite meta-data with newer version")
					r.Overwrite = true
				} else {
					l.Debug("meta-data is not newer than source, skipping")
					continue
				}
			} else {
				// If its not meta, and the file exists then flatly decline to upload
				// the file -- its not safe.
				// TODO: allow for artifact rebuilding
				l.Debug("remote location already has file")
				continue
			}
		}
		if err := r.Minio.putter(ctx, r.Bucket, k, v); err != nil {
			l.WithField("err", err).Error("failed upload, tainting build")
			e = fmt.Errorf("upload failed with at least one error: %w", err)
		}
	}
	return e
}

// isKnownBuildMeta checks if n is known and should be fetched and returned
// by pods via Minio.
func isKnownBuildMeta(n string) bool {
	// check for specially named files
	if strings.HasPrefix(n, "manifest-lock.generated") ||
		n == "ostree-commit-object" ||
		n == "commitmeta.json" ||
		n == "coreos-assembler-config-git.json" {
		return true
	}
	// check for meta*json files
	if cosa.IsMetaJSON(n) {
		return true
	}

	if n == "builds.json" {
		return true
	}

	return false
}

var (
	// sudoBashCmd is used for shelling out to comamnds.
	sudoBashCmd = []string{"sudo", "bash", "-c"}

	// bashCmd is used for shelling out to commands
	bashCmd = []string{"bash", "-c"}
)

// uploadPathAsTarBall returns a path as a tarball to minio server. This uses a shell call out
// since we need to elevate permissions via sudo (bug in Golang <1.16 prevents elevating privs).
// Gangplank runs as the builder user normally and since some files are written by root, Gangplank
// will get permission denied.
//
// The tarball creation will be done relative to workDir. If workDir is an empty string, it will default
// to the current working directory.
func uploadPathAsTarBall(ctx context.Context, bucket, object, path, workDir string, sudo bool, r *Return) error {
	tmpD, err := ioutil.TempDir("", "tarball")
	if err != nil {
		return err
	}

	tmpf, err := ioutil.TempFile("", "")
	if err != nil {
		return err
	}
	tmpf.Close()
	_ = os.Remove(tmpf.Name()) // we just want the file name
	tmpfgz := fmt.Sprintf("%s.gz", tmpf.Name())

	defer func() {
		_ = os.RemoveAll(tmpD)
		_ = os.Remove(tmpf.Name())
		_ = os.Remove(tmpfgz)
	}()

	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	prefix := bashCmd
	if sudo {
		prefix = sudoBashCmd
	}

	// Here be hacks: we set the tarball to be world-read writeable so that the
	// defer above can clean-up without requiring root.
	args := append(
		prefix,
		fmt.Sprintf("umask 000; tar -cf %s %s; stat %s; gzip --fast %s;",
			tmpf.Name(), path, tmpf.Name(), tmpf.Name()))
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	l := log.WithFields(log.Fields{
		"path":     path,
		"tarball":  tmpf,
		"dest url": fmt.Sprintf("http://%s:%d/%s/%s", r.Minio.Host, r.Minio.Port, bucket, object),
	})

	l.WithField("shell command", args).Info("creating tarball")
	if err := cmd.Run(); err != nil {
		l.WithError(err).Error("failed to create tarball")
		return err
	}

	if err := r.Minio.putter(ctx, bucket, object, tmpfgz); err != nil {
		l.WithError(err).WithField("path", path).Error("failed pushing tarball to minio")
	}

	return nil
}

// uploadReturnFiles uploads requested files to the minio server.
func uploadReturnFiles(ctx context.Context, bucket string, files []string, r *Return) error {
	if r.Minio == nil {
		return nil
	}
	upload := make(map[string]string)

	// Grab any files that were requested to be returned and upload
	// them.
	for _, f := range files {
		upKey := filepath.Join(bucket, f)
		srcPath := filepath.Join(cosaSrvDir, f)
		upload[upKey] = srcPath
	}

	var e error = nil
	for k, v := range upload {
		if r.KeyPrefix != "" {
			k = filepath.Join(r.KeyPrefix, k)
		}

		l := log.WithFields(log.Fields{
			"host":          r.Minio.Host,
			"file":          v,
			"remote/bucket": r.Bucket,
			"remote/key":    k,
		})

		if err := r.Minio.putter(ctx, r.Bucket, k, v); err != nil {
			l.WithField("err", err).Error("failed upload of requested file")
			e = fmt.Errorf("upload failed with at least one error: %w", err)
		}
	}
	return e
}
