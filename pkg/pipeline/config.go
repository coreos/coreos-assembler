package pipeline

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// Config is default pipeline configuration, stored in
// src/config/pipeline.yaml
type Config struct {
	// Compressor can currently be one of "xz" or "gzip".  The default
	// is gzip.
	Compressor string `json:"compressor"`
}

func ReadConfig(workdir string) (*Config, error) {
	buf, err := ioutil.ReadFile("/usr/lib/coreos-assembler/pipeline-default.yaml")
	if err != nil {
		return nil, err
	}

	var config Config
	dec := yaml.NewDecoder(bytes.NewReader(buf))
	dec.KnownFields(true)
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}

	buf, err = ioutil.ReadFile(filepath.Join(workdir, "src/config/pipeline.yaml"))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(buf))
		dec.KnownFields(true)
		if err := dec.Decode(&config); err != nil {
			return nil, err
		}
	}

	return &config, nil
}
