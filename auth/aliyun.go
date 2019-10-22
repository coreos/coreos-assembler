// Copyright 2019 Red Hat
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

const AliyunConfigPath = ".aliyun/config.json"

type AliyunProfile struct {
	Name            string `json:"name"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	Region          string `json:"region_id"`
}

type AliyunConfig struct {
	Profiles []AliyunProfile `json:"profiles"`
}

// ReadAliyunConfig decodes an aliyun config file
//
// If path is empty, $HOME/.aliyun/config.json is read.
func ReadAliyunConfig(path string) (map[string]AliyunProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, AliyunConfigPath)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var config AliyunConfig
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return nil, err
	}
	if len(config.Profiles) == 0 {
		return nil, fmt.Errorf("aliyun config %q contains no profiles", path)
	}

	retProfiles := make(map[string]AliyunProfile)
	for _, p := range config.Profiles {
		retProfiles[p.Name] = p
	}

	return retProfiles, nil
}
