// Copyright 2017 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/coreos/pkg/capnslog"
	"github.com/go-ini/ini"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "auth")
)

const ociConfigPath = ".oci/config"

// OCIProfile represents a parsed OCI profile.
type OCIProfile struct {
	TenancyID          string `ini:"tenancy"`
	UserID             string `ini:"user"`
	Fingerprint        string `ini:"fingerprint"`
	KeyFile            string `ini:"key_file"`
	PrivateKeyPassword string `ini:"pass_phrase"`
	Region             string `ini:"region"`

	// Non-Standard Keys
	CompartmentID string `ini:"compartment"`
}

// ReadOCIConfig builds an OCIProfile from the OCIConfig files.
// It takes a path (which defaults to $HOME/.oci/config) and will parse
// the standard configuration, defined at:
// https://docs.us-phoenix-1.oraclecloud.com/Content/API/Concepts/sdkconfig.htm
//
// It will then attempt to parse a .mantle file in the same directory (defaulting
// to ($HOME/.oci/config.mantle) to allow overrides & other variable
// definitions not used in the standard configuration.
//
// The parsing is done with via InsensitiveLoad which ignores casing for both
// section & key names.
func ReadOCIConfig(path string) (map[string]OCIProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, ociConfigPath)
	}

	profiles := make(map[string]OCIProfile)

	// first parse the standard oracle config
	cfg, err := ini.InsensitiveLoad(path)
	if err != nil {
		return nil, fmt.Errorf("Loading OCI config: %v", err)
	}

	for _, section := range cfg.Sections() {
		p := OCIProfile{}
		err = section.MapTo(&p)
		if err != nil {
			return nil, err
		}

		profiles[strings.ToLower(section.Name())] = p
	}

	// attempt to parse the mantle config
	cfg, err = ini.InsensitiveLoad(fmt.Sprintf("%s.mantle", path))
	if err == nil {
		for _, section := range cfg.Sections() {
			p, _ := profiles[strings.ToLower(section.Name())]
			err = section.MapTo(&p)
			profiles[strings.ToLower(section.Name())] = p
		}
	} else {
		plog.Warningf("reading %s: %v", fmt.Sprintf("%s.mantle", path), err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("OCI config %q contains no profiles", path)
	}

	return profiles, nil
}
