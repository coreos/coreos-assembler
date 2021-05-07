package spec

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Override describes RPMs or Tarballs to include as an override in the OSTree compose.
type Override struct {
	// URI is a string prefixed with "file://" or "http(s)://" and a path.
	URI string `yaml:"uri,omitempty" json:"uri,omitempty"`

	// Rpm indicates that the file is RPM and should be placed in overrides/rpm
	Rpm *bool `yaml:"rpm,omitempty" json:"rpm,omitempty"`

	// Tarball indicates that the file is a tarball and will be extracted to overrides.
	Tarball *bool `yaml:"tarball,omitempty" json:"tarball,omitempty"`

	// Tarball type is an override Tarball type
	TarballType *string `yaml:"tarball_type,omitempty" json:"tarball_type,omitempty"`
}

const (
	TarballTypeAll    = "all"
	TarballTypeRpms   = "rpms"
	TarballTypeRpm    = "rpm"
	TarballTypeRootfs = "rootfs"
	overrideBasePath  = "overrides"
)

// writePath gets the path that the file should be extract to
func (o *Override) writePath(basePath string) (string, error) {
	obase := filepath.Join(basePath, overrideBasePath)

	if o.Rpm != nil && *o.Rpm {
		return filepath.Join(obase, "rpm", filepath.Base(o.URI)), nil
	}

	if o.Tarball == nil {
		return "", fmt.Errorf("override must be either tarball or RPM")
	}

	// assume that the tarball type is all
	if o.TarballType == nil {
		return obase, nil
	}

	switch *o.TarballType {
	case TarballTypeAll:
		return obase, nil
	case TarballTypeRpms, TarballTypeRpm:
		return filepath.Join(obase, TarballTypeRpm), nil
	case TarballTypeRootfs:
		return filepath.Join(obase, TarballTypeRootfs), nil
	default:
		return "", fmt.Errorf("tarball type %s is unknown", *o.TarballType)
	}
}

// TarDecompressorFunc is a function that handles decompressing a file.
type TarDecompressorFunc func(io.ReadCloser, string) error

// Fetch reads the source and writes it to disk. The decompressor function
// is likely lazy, but allows for testing.
func (o *Override) Fetch(l *log.Entry, path string, wf TarDecompressorFunc) error {
	nl := l.WithFields(log.Fields{
		"uri":  o.URI,
		"path": path,
	})

	if o.URI == "" {
		return errors.New("uri is undefined for override")
	}

	parts := strings.Split(o.URI, "://")
	if len(parts) == 1 {
		return fmt.Errorf("path lack URI identifer: %s", o.URI)
	}

	writePath, err := o.writePath(path)
	if err != nil {
		return err
	}

	basePath := writePath
	if o.Rpm != nil && *o.Rpm {
		basePath = filepath.Dir(basePath)
	}

	nl = nl.WithField("target path", writePath)

	nl.Warn("creating target dir")
	if err := os.MkdirAll(basePath, 0755); err != nil {
		nl.WithError(err).Error("failed to create target path")
		return fmt.Errorf("unable to create target dir %s: %v", basePath, err)
	}

	var in io.ReadCloser
	switch parts[0] {
	case "file":
		f, err := os.Open(parts[1])
		if err != nil {
			l.WithError(err).Error("failed to open file")
			return err
		}
		in = f
	case "https", "http":
		resp, err := http.Get(o.URI)
		if err != nil {
			l.WithError(err).Error("failed to open remote address")
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 305 {
			return fmt.Errorf("unable to fetch resource status code %d: %s", resp.StatusCode, resp.Status)
		}
		in = resp.Body
	}
	defer in.Close()

	// If this is a tarball, run the decompressor func and bail.
	if o.Tarball != nil && *o.Tarball {
		nl.Info("extracting uri to path")
		return wf(in, writePath)
	}

	// Otherwise this an RPM -- treat it like a generic file
	f, err := os.Create(writePath)
	if err != nil {
		return err
	}
	defer f.Close()

	nl.Info("writing uri to path")
	_, err = io.Copy(f, in)
	nl.Info("success")
	return err
}
