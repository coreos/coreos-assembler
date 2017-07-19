// Copyright 2016 CoreOS, Inc.
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

package conf

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	ct "github.com/coreos/container-linux-config-transpiler/config"
	cci "github.com/coreos/coreos-cloudinit/config"
	v1 "github.com/coreos/ignition/config/v1"
	v1types "github.com/coreos/ignition/config/v1/types"
	v2 "github.com/coreos/ignition/config/v2_1"
	v2types "github.com/coreos/ignition/config/v2_1/types"
	"github.com/coreos/ignition/config/validate/report"
	"github.com/coreos/pkg/capnslog"
	"golang.org/x/crypto/ssh/agent"
)

type kind int

const (
	kindEmpty kind = iota
	kindCloudConfig
	kindIgnition
	kindContainerLinuxConfig
	kindScript
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/conf")

// UserData is an immutable, unvalidated configuration for a Container Linux
// machine.
type UserData struct {
	kind kind
	data string
}

// Conf is a configuration for a Container Linux machine. It may be either a
// coreos-cloudconfig or an ignition configuration.
type Conf struct {
	ignitionV1  *v1types.Config
	ignitionV2  *v2types.Config
	cloudconfig *cci.CloudConfig
	script      string
}

func Empty() *UserData {
	return &UserData{
		kind: kindEmpty,
	}
}

func ContainerLinuxConfig(data string) *UserData {
	return &UserData{
		kind: kindContainerLinuxConfig,
		data: data,
	}
}

func Ignition(data string) *UserData {
	return &UserData{
		kind: kindIgnition,
		data: data,
	}
}

func CloudConfig(data string) *UserData {
	return &UserData{
		kind: kindCloudConfig,
		data: data,
	}
}

func Script(data string) *UserData {
	return &UserData{
		kind: kindScript,
		data: data,
	}
}

func Unknown(data string) *UserData {
	u := &UserData{
		data: data,
	}

	_, _, err := v2.Parse([]byte(data))
	switch err {
	case v2.ErrEmpty:
		u.kind = kindEmpty
	case v2.ErrCloudConfig:
		u.kind = kindCloudConfig
	case v2.ErrScript:
		u.kind = kindScript
	default:
		// we don't autodetect Container Linux configs
		u.kind = kindIgnition
	}

	return u
}

// Performs a string substitution and returns a new UserData.
func (u *UserData) Subst(old, new string) *UserData {
	ret := *u
	ret.data = strings.Replace(u.data, old, new, -1)
	return &ret
}

func (u *UserData) IsIgnition() bool {
	return u.kind == kindIgnition
}

// Render parses userdata and returns a new Conf. It returns an error if the
// userdata can't be parsed.
func (u *UserData) Render() (*Conf, error) {
	c := &Conf{}

	switch u.kind {
	case kindEmpty:
		// empty, noop
	case kindCloudConfig:
		var err error
		c.cloudconfig, err = cci.NewCloudConfig(u.data)
		if err != nil {
			return nil, err
		}
	case kindScript:
		// pass through scripts unmodified, you are on your own.
		c.script = u.data
	case kindIgnition:
		// Reports collapse errors to their underlying strings
		haveEntry := func(report report.Report, err error) bool {
			for _, entry := range report.Entries {
				if err.Error() == entry.Message {
					return true
				}
			}
			return false
		}

		ignc, report, err := v2.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV2 = &ignc
		} else if haveEntry(report, v2types.ErrInvalidVersion) {
			// version 1 config
			var ignc v1types.Config
			ignc, err = v1.Parse([]byte(u.data))
			if err != nil {
				return nil, err
			}
			c.ignitionV1 = &ignc
		} else {
			plog.Errorf("invalid userdata: %v", report)
			return nil, err
		}
	case kindContainerLinuxConfig:
		clc, ast, report := ct.Parse([]byte(u.data))
		if report.IsFatal() {
			return nil, fmt.Errorf("parsing Container Linux config: %s", report)
		} else if len(report.Entries) > 0 {
			plog.Warningf("parsing Container Linux config: %s", report)
		}

		// TODO(bgilbert): substitute cloud-specific variables via ct
		ignc, report := ct.ConvertAs2_0(clc, "", ast)
		if report.IsFatal() {
			return nil, fmt.Errorf("rendering Container Linux config: %s", report)
		} else if len(report.Entries) > 0 {
			plog.Warningf("rendering Container Linux config: %s", report)
		}

		// ct still returns 2.0 configs. Convert to 2.1.
		buf, err := json.Marshal(ignc)
		if err != nil {
			return nil, fmt.Errorf("serializing Container Linux config: %v", err)
		}
		return Ignition(string(buf)).Render()
	default:
		panic("invalid kind")
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
	} else if c.script != "" {
		return c.script
	}

	return ""
}

// WriteFile writes the userdata in Conf to a local file.
func (c *Conf) WriteFile(name string) error {
	return ioutil.WriteFile(name, []byte(c.String()), 0666)
}

// Bytes returns the serialized userdata in Conf.
func (c *Conf) Bytes() []byte {
	return []byte(c.String())
}

func (c *Conf) copyKeysIgnitionV1(keys []*agent.Key) {
	c.ignitionV1.Passwd.Users = append(c.ignitionV1.Passwd.Users, v1types.User{
		Name:              "core",
		SSHAuthorizedKeys: keysToStrings(keys),
	})
}

func (c *Conf) copyKeysIgnitionV2(keys []*agent.Key) {
	var keyObjs []v2types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v2types.SSHAuthorizedKey(key.String()))
	}
	c.ignitionV2.Passwd.Users = append(c.ignitionV2.Passwd.Users, v2types.PasswdUser{
		Name:              "core",
		SSHAuthorizedKeys: keyObjs,
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

// IsIgnition returns true if the config is for Ignition.
func (c *Conf) IsIgnition() bool {
	return c.ignitionV1 != nil || c.ignitionV2 != nil
}
