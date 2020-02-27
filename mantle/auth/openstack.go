// Copyright 2018 Red Hat
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

const OpenStackConfigPath = ".config/openstack.json"

type OpenStackProfile struct {
	AuthURL    string `json:"auth_url"`
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Username   string `json:"username"`
	Password   string `json:"password"`

	//Optional
	Domain         string `json:"user_domain"`
	FloatingIPPool string `json:"floating_ip_pool"`
	Region         string `json:"region_name"`
}

// ReadOpenStackConfig decodes an OpenStack config file,
// which is a custom format used by Mantle to hold OpenStack
// server information.
//
// If path is empty, $HOME/.config/openstack.json is read.
func ReadOpenStackConfig(path string) (map[string]OpenStackProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(user.HomeDir, OpenStackConfigPath)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var profiles map[string]OpenStackProfile
	if err := json.NewDecoder(f).Decode(&profiles); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("OpenStack config %q contains no profiles", path)
	}

	return profiles, nil
}
