// Copyright 2015 CoreOS, Inc.
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

package platform

import (
	"encoding/json"

	cci "github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
	v2 "github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/ignition/config"
	v2types "github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/ignition/config/types"
	v1 "github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/ignition/config/v1"
	v1types "github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/ignition/config/v1/types"
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh/agent"
)

// Conf is a configuration for a CoreOS machine. It may be either a
// coreos-cloudconfig or an ignition configuration.
type Conf struct {
	ignitionV1  *v1types.Config
	ignitionV2  *v2types.Config
	cloudconfig *cci.CloudConfig
}

// NewConf parses userdata and returns a new Conf. It returns an error if the
// userdata can't be parsed as a coreos-cloudinit or ignition configuration.
func NewConf(userdata string) (*Conf, error) {
	c := &Conf{}

	ignc, err := v2.Parse([]byte(userdata))
	switch err {
	case v2.ErrEmpty:
		// empty, noop
	case v2.ErrCloudConfig:
		// fall back to cloud-config
		c.cloudconfig, err = cci.NewCloudConfig(userdata)
		if err != nil {
			return nil, err
		}
	case v2.ErrDeprecated:
		if ignc, err := v1.Parse([]byte(userdata)); err == nil {
			c.ignitionV1 = &ignc
		} else {
			return nil, err
		}
	case nil:
		c.ignitionV2 = &ignc
	default:
		// some other error (invalid json, script)
		return nil, err
	}

	return c, nil
}

// String returns the string representation of the userdata in Conf.
func (c *Conf) String() string {
	if c.ignitionV1 != nil {
		buf, _ := json.Marshal(c.ignitionV1)
		return string(buf)
	} else if c.ignitionV2 != nil {
		buf, _ := json.Marshal(c.ignitionV2)
		return string(buf)
	} else if c.cloudconfig != nil {
		return c.cloudconfig.String()
	}

	return ""
}

func (c *Conf) copyKeysIgnitionV1(keys []*agent.Key) {
	c.ignitionV1.Passwd.Users = append(c.ignitionV1.Passwd.Users, v1types.User{
		Name:              "core",
		SSHAuthorizedKeys: keysToStrings(keys),
	})
}

func (c *Conf) copyKeysIgnitionV2(keys []*agent.Key) {
	c.ignitionV2.Passwd.Users = append(c.ignitionV2.Passwd.Users, v2types.User{
		Name:              "core",
		SSHAuthorizedKeys: keysToStrings(keys),
	})
}

func (c *Conf) copyKeysCloudConfig(keys []*agent.Key) {
	c.cloudconfig.SSHAuthorizedKeys = append(c.cloudconfig.SSHAuthorizedKeys, keysToStrings(keys)...)
}

// CopyKeys copies public keys from agent ag into the configuration to the
// appropriate configuration section for the core user.
func (c *Conf) CopyKeys(keys []*agent.Key) {
	if c.ignitionV1 != nil {
		c.copyKeysIgnitionV1(keys)
	} else if c.ignitionV2 != nil {
		c.copyKeysIgnitionV2(keys)
	} else if c.cloudconfig != nil {
		c.copyKeysCloudConfig(keys)
	}
}

func keysToStrings(keys []*agent.Key) (keyStrs []string) {
	for _, key := range keys {
		keyStrs = append(keyStrs, key.String())
	}
	return
}
