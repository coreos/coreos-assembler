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

const ESXConfigPath = ".config/esx.json"

// ESXProfile represents a parsed ESX profile. This is a custom format
// specific to Mantle.
type ESXProfile struct {
	Server   string `json:"server"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// ReadESXConfig decodes a ESX config file, which is a custom format
// used by Mantle to hold ESX server information.
//
// If path is empty, $HOME/.config/esx.json is read.
func ReadESXConfig(path string) (map[string]ESXProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, ESXConfigPath)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var profiles map[string]ESXProfile
	if err := json.NewDecoder(f).Decode(&profiles); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("ESX config %q contains no profiles", path)
	}

	return profiles, nil
}
