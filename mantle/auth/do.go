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
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

const DOConfigPath = ".config/digitalocean.json"

// DOProfile represents a parsed DigitalOcean profile.  This is a custom
// format specific to Mantle.
type DOProfile struct {
	AccessToken string `json:"token"`
}

// ReadDOConfig decodes a DigitalOcean config file, which is a custom format
// used by Mantle to hold personal access tokens.
//
// If path is empty, $HOME/.config/digitalocean.json is read.
func ReadDOConfig(path string) (map[string]DOProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, DOConfigPath)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var profiles map[string]DOProfile
	if err := json.NewDecoder(f).Decode(&profiles); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("DigitalOcean config %q contains no profiles", path)
	}

	return profiles, nil
}
