// Copyright 2016-2018 CoreOS, Inc.
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
	"github.com/coreos/go-semver/semver"
	ign "github.com/coreos/ignition/config"
	v1 "github.com/coreos/ignition/config/v1"
	v1types "github.com/coreos/ignition/config/v1/types"
	v2 "github.com/coreos/ignition/config/v2_0"
	v2types "github.com/coreos/ignition/config/v2_0/types"
	v21 "github.com/coreos/ignition/config/v2_1"
	v21types "github.com/coreos/ignition/config/v2_1/types"
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
	kind      kind
	data      string
	extraKeys []*agent.Key // SSH keys to be injected during rendering
}

// Conf is a configuration for a Container Linux machine. It may be either a
// coreos-cloudconfig or an ignition configuration.
type Conf struct {
	ignitionV1  *v1types.Config
	ignitionV2  *v2types.Config
	ignitionV21 *v21types.Config
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

	_, _, err := v21.Parse([]byte(data))
	switch err {
	case v21.ErrEmpty:
		u.kind = kindEmpty
	case v21.ErrCloudConfig:
		u.kind = kindCloudConfig
	case v21.ErrScript:
		u.kind = kindScript
	default:
		// Guess whether this is an Ignition config or a CLC.
		// This treats an invalid Ignition config as a CLC, and a
		// CLC in the JSON subset of YAML as an Ignition config.
		var decoded interface{}
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			u.kind = kindContainerLinuxConfig
		} else {
			u.kind = kindIgnition
		}
	}

	return u
}

// Contains returns true if the UserData contains the specified string.
func (u *UserData) Contains(substr string) bool {
	return strings.Contains(u.data, substr)
}

// Performs a string substitution and returns a new UserData.
func (u *UserData) Subst(old, new string) *UserData {
	ret := *u
	ret.data = strings.Replace(u.data, old, new, -1)
	return &ret
}

// Adds an SSH key and returns a new UserData.
func (u *UserData) AddKey(key agent.Key) *UserData {
	ret := *u
	ret.extraKeys = append(ret.extraKeys, &key)
	return &ret
}

func (u *UserData) IsIgnitionCompatible() bool {
	return u.kind == kindIgnition || u.kind == kindContainerLinuxConfig
}

// Render parses userdata and returns a new Conf. It returns an error if the
// userdata can't be parsed.
func (u *UserData) Render(ctPlatform string) (*Conf, error) {
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
		ver, err := ign.Version([]byte(u.data))
		// process indeterminable configs with the current version
		// so we can get parse errors
		if err != nil && err != ign.ErrVersionIndeterminable {
			return nil, err
		}

		switch ver {
		default:
			// an Ignition 2.1 config, or an indeterminable one
			ignc, report, err := v21.Parse([]byte(u.data))
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return nil, err
			}
			c.ignitionV21 = &ignc
		case semver.Version{Major: 2}:
			ignc, report, err := v2.Parse([]byte(u.data))
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return nil, err
			}
			c.ignitionV2 = &ignc
		case semver.Version{Major: 1}:
			ignc, err := v1.Parse([]byte(u.data))
			if err != nil {
				return nil, err
			}
			c.ignitionV1 = &ignc
		}
	case kindContainerLinuxConfig:
		clc, ast, report := ct.Parse([]byte(u.data))
		if report.IsFatal() {
			return nil, fmt.Errorf("parsing Container Linux config: %s", report)
		} else if len(report.Entries) > 0 {
			plog.Warningf("parsing Container Linux config: %s", report)
		}

		ignc, report := ct.Convert(clc, ctPlatform, ast)
		if report.IsFatal() {
			return nil, fmt.Errorf("rendering Container Linux config for platform %q: %s", ctPlatform, report)
		} else if len(report.Entries) > 0 {
			plog.Warningf("rendering Container Linux config: %s", report)
		}

		c.ignitionV21 = &ignc
	default:
		panic("invalid kind")
	}

	if len(u.extraKeys) > 0 {
		// not a no-op in the zero-key case
		c.CopyKeys(u.extraKeys)
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
	} else if c.ignitionV21 != nil {
		buf, _ := json.Marshal(c.ignitionV21)
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

func (c *Conf) addSystemdUnitV1(name, contents string, enable bool) {
	c.ignitionV1.Systemd.Units = append(c.ignitionV1.Systemd.Units, v1types.SystemdUnit{
		Name:     v1types.SystemdUnitName(name),
		Contents: contents,
		Enable:   enable,
	})
}

func (c *Conf) addSystemdUnitV2(name, contents string, enable bool) {
	c.ignitionV2.Systemd.Units = append(c.ignitionV2.Systemd.Units, v2types.SystemdUnit{
		Name:     v2types.SystemdUnitName(name),
		Contents: contents,
		Enable:   enable,
	})
}

func (c *Conf) addSystemdUnitV21(name, contents string, enable bool) {
	c.ignitionV21.Systemd.Units = append(c.ignitionV21.Systemd.Units, v21types.Unit{
		Name:     name,
		Contents: contents,
		Enabled:  &enable,
	})
}

func (c *Conf) addSystemdUnitCloudConfig(name, contents string, enable bool) {
	c.cloudconfig.CoreOS.Units = append(c.cloudconfig.CoreOS.Units, cci.Unit{
		Name:    name,
		Content: contents,
		Enable:  enable,
	})
}

func (c *Conf) AddSystemdUnit(name, contents string, enable bool) {
	if c.ignitionV1 != nil {
		c.addSystemdUnitV1(name, contents, enable)
	} else if c.ignitionV2 != nil {
		c.addSystemdUnitV2(name, contents, enable)
	} else if c.ignitionV21 != nil {
		c.addSystemdUnitV21(name, contents, enable)
	} else if c.cloudconfig != nil {
		c.addSystemdUnitCloudConfig(name, contents, enable)
	}
}

func (c *Conf) addSystemdDropinV1(service, name, contents string) {
	for i, unit := range c.ignitionV1.Systemd.Units {
		if unit.Name == v1types.SystemdUnitName(service) {
			unit.DropIns = append(unit.DropIns, v1types.SystemdUnitDropIn{
				Name:     v1types.SystemdUnitDropInName(name),
				Contents: contents,
			})
			c.ignitionV1.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV1.Systemd.Units = append(c.ignitionV1.Systemd.Units, v1types.SystemdUnit{
		Name: v1types.SystemdUnitName(service),
		DropIns: []v1types.SystemdUnitDropIn{
			{
				Name:     v1types.SystemdUnitDropInName(name),
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinV2(service, name, contents string) {
	for i, unit := range c.ignitionV2.Systemd.Units {
		if unit.Name == v2types.SystemdUnitName(service) {
			unit.DropIns = append(unit.DropIns, v2types.SystemdUnitDropIn{
				Name:     v2types.SystemdUnitDropInName(name),
				Contents: contents,
			})
			c.ignitionV2.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV2.Systemd.Units = append(c.ignitionV2.Systemd.Units, v2types.SystemdUnit{
		Name: v2types.SystemdUnitName(service),
		DropIns: []v2types.SystemdUnitDropIn{
			{
				Name:     v2types.SystemdUnitDropInName(name),
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinV21(service, name, contents string) {
	for i, unit := range c.ignitionV21.Systemd.Units {
		if unit.Name == service {
			unit.Dropins = append(unit.Dropins, v21types.Dropin{
				Name:     name,
				Contents: contents,
			})
			c.ignitionV21.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV21.Systemd.Units = append(c.ignitionV21.Systemd.Units, v21types.Unit{
		Name: service,
		Dropins: []v21types.Dropin{
			{
				Name:     name,
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinCloudConfig(service, name, contents string) {
	for i, unit := range c.cloudconfig.CoreOS.Units {
		if unit.Name == service {
			unit.DropIns = append(unit.DropIns, cci.UnitDropIn{
				Name:    name,
				Content: contents,
			})
			c.cloudconfig.CoreOS.Units[i] = unit
			return
		}
	}
	c.cloudconfig.CoreOS.Units = append(c.cloudconfig.CoreOS.Units, cci.Unit{
		Name: service,
		DropIns: []cci.UnitDropIn{
			{
				Name:    name,
				Content: contents,
			},
		},
	})
}

func (c *Conf) AddSystemdUnitDropin(service, name, contents string) {
	if c.ignitionV1 != nil {
		c.addSystemdDropinV1(service, name, contents)
	} else if c.ignitionV2 != nil {
		c.addSystemdDropinV2(service, name, contents)
	} else if c.ignitionV21 != nil {
		c.addSystemdDropinV21(service, name, contents)
	} else if c.cloudconfig != nil {
		c.addSystemdDropinCloudConfig(service, name, contents)
	}
}

func (c *Conf) copyKeysIgnitionV1(keys []*agent.Key) {
	keyStrs := keysToStrings(keys)
	for i := range c.ignitionV1.Passwd.Users {
		user := &c.ignitionV1.Passwd.Users[i]
		if user.Name == "core" {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyStrs...)
			return
		}
	}
	c.ignitionV1.Passwd.Users = append(c.ignitionV1.Passwd.Users, v1types.User{
		Name:              "core",
		SSHAuthorizedKeys: keyStrs,
	})
}

func (c *Conf) copyKeysIgnitionV2(keys []*agent.Key) {
	keyStrs := keysToStrings(keys)
	for i := range c.ignitionV2.Passwd.Users {
		user := &c.ignitionV2.Passwd.Users[i]
		if user.Name == "core" {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyStrs...)
			return
		}
	}
	c.ignitionV2.Passwd.Users = append(c.ignitionV2.Passwd.Users, v2types.User{
		Name:              "core",
		SSHAuthorizedKeys: keyStrs,
	})
}

func (c *Conf) copyKeysIgnitionV21(keys []*agent.Key) {
	var keyObjs []v21types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v21types.SSHAuthorizedKey(key.String()))
	}
	for i := range c.ignitionV21.Passwd.Users {
		user := &c.ignitionV21.Passwd.Users[i]
		if user.Name == "core" {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyObjs...)
			return
		}
	}
	c.ignitionV21.Passwd.Users = append(c.ignitionV21.Passwd.Users, v21types.PasswdUser{
		Name:              "core",
		SSHAuthorizedKeys: keyObjs,
	})
}

func (c *Conf) copyKeysCloudConfig(keys []*agent.Key) {
	c.cloudconfig.SSHAuthorizedKeys = append(c.cloudconfig.SSHAuthorizedKeys, keysToStrings(keys)...)
}

func (c *Conf) copyKeysScript(keys []*agent.Key) {
	keyString := strings.Join(keysToStrings(keys), "\n")
	c.script = strings.Replace(c.script, "@SSH_KEYS@", keyString, -1)
}

// CopyKeys copies public keys from agent ag into the configuration to the
// appropriate configuration section for the core user.
func (c *Conf) CopyKeys(keys []*agent.Key) {
	if c.ignitionV1 != nil {
		c.copyKeysIgnitionV1(keys)
	} else if c.ignitionV2 != nil {
		c.copyKeysIgnitionV2(keys)
	} else if c.ignitionV21 != nil {
		c.copyKeysIgnitionV21(keys)
	} else if c.cloudconfig != nil {
		c.copyKeysCloudConfig(keys)
	} else if c.script != "" {
		c.copyKeysScript(keys)
	}
}

func keysToStrings(keys []*agent.Key) (keyStrs []string) {
	for _, key := range keys {
		keyStrs = append(keyStrs, key.String())
	}
	return
}

// IsIgnition returns true if the config is for Ignition.
// Returns false in the case of empty configs as on most platforms,
// this will default back to cloudconfig
func (c *Conf) IsIgnition() bool {
	return c.ignitionV1 != nil || c.ignitionV2 != nil || c.ignitionV21 != nil
}

func (c *Conf) IsEmpty() bool {
	return !c.IsIgnition() && c.cloudconfig == nil && c.script == ""
}
