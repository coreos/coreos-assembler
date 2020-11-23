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
	"net/url"
	"os"
	"reflect"
	"strings"

	ignconverterv30tov22 "github.com/coreos/ign-converter/translate/v30tov22"
	ignconverterv31tov24 "github.com/coreos/ign-converter/translate/v31tov24"
	ignconverterv32tov24 "github.com/coreos/ign-converter/translate/v32tov24"

	ct "github.com/coreos/container-linux-config-transpiler/config"
	systemdunit "github.com/coreos/go-systemd/unit"
	ignerr "github.com/coreos/ignition/config/shared/errors"
	v2 "github.com/coreos/ignition/config/v2_0"
	v2types "github.com/coreos/ignition/config/v2_0/types"
	v21 "github.com/coreos/ignition/config/v2_1"
	v21types "github.com/coreos/ignition/config/v2_1/types"
	v22 "github.com/coreos/ignition/config/v2_2"
	v22types "github.com/coreos/ignition/config/v2_2/types"
	v23 "github.com/coreos/ignition/config/v2_3"
	v23types "github.com/coreos/ignition/config/v2_3/types"
	v24 "github.com/coreos/ignition/config/v2_4"
	v24types "github.com/coreos/ignition/config/v2_4/types"
	ignvalidate "github.com/coreos/ignition/config/validate"
	ign3err "github.com/coreos/ignition/v2/config/shared/errors"
	v3 "github.com/coreos/ignition/v2/config/v3_0"
	v3types "github.com/coreos/ignition/v2/config/v3_0/types"
	v31 "github.com/coreos/ignition/v2/config/v3_1"
	v31types "github.com/coreos/ignition/v2/config/v3_1/types"
	v32 "github.com/coreos/ignition/v2/config/v3_2"
	v32types "github.com/coreos/ignition/v2/config/v3_2/types"
	v33exp "github.com/coreos/ignition/v2/config/v3_3_experimental"
	v33exptypes "github.com/coreos/ignition/v2/config/v3_3_experimental/types"
	ign3validate "github.com/coreos/ignition/v2/config/validate"
	"github.com/coreos/pkg/capnslog"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/crypto/ssh/agent"
)

type kind int

const (
	kindEmpty kind = iota
	kindIgnition
	kindContainerLinuxConfig
)

type systemdUnitState int

const (
	NoState systemdUnitState = iota
	Enable
	Mask
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/conf")

// UserData is an immutable, unvalidated configuration for a CoreOS
// machine.
type UserData struct {
	kind      kind
	data      string
	extraKeys []*agent.Key // SSH keys to be injected during rendering
}

// Conf is a configuration for a CoreOS machine. It's Ignition 2 or 3
type Conf struct {
	ignitionV2  *v2types.Config
	ignitionV21 *v21types.Config
	ignitionV22 *v22types.Config
	ignitionV23 *v23types.Config
	ignitionV24 *v24types.Config
	ignitionV3  *v3types.Config
	ignitionV31 *v31types.Config
	ignitionV32 *v32types.Config

	ignitionV33exp *v33exptypes.Config
}

// Empty creates a completely empty configuration. Any configuration addition
// applied to an empty config triggers a panic.
func Empty() *UserData {
	return &UserData{
		kind: kindEmpty,
	}
}

// ContainerLinuxConfig creates a Container Linux Config Userdata struct from
// the provided string.
func ContainerLinuxConfig(data string) *UserData {
	return &UserData{
		kind: kindContainerLinuxConfig,
		data: data,
	}
}

// EmptyIgnition returns an a default empty config using the latest
// stable supported Ignition spec.
func EmptyIgnition() *UserData {
	return Ignition("")
}

// Ignition returns an Ignition UserData struct from the provided string. If the
// given string is empty, it will create a default empty config using the latest
// stable supported Ignition spec.
func Ignition(data string) *UserData {
	if data == "" {
		data = `{"ignition": {"version": "3.2.0"}}`
	}
	return &UserData{
		kind: kindIgnition,
		data: data,
	}
}

func Unknown(data string) *UserData {
	u := &UserData{
		data: data,
	}

	_, _, err := v22.Parse([]byte(data))
	switch err {
	case ignerr.ErrEmpty:
		u.kind = kindEmpty
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

// Subst performs a string substitution and returns a new UserData.
func (u *UserData) Subst(old, new string) *UserData {
	ret := *u
	ret.data = strings.Replace(u.data, old, new, -1)
	return &ret
}

// AddKey adds an SSH key and returns a new UserData.
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
func (u *UserData) Render(ignv2 bool) (*Conf, error) {
	return u.RenderForCtPlatform(ignv2, "")
}

// RenderForCtPlatform parses userdata and returns a new Conf. It returns an error if the
// userdata can't be parsed.
func (u *UserData) RenderForCtPlatform(ignv2 bool, ctPlatform string) (*Conf, error) {
	c := &Conf{}

	renderIgnition := func() error {
		// Try each known version in turn.  Newer parsers will
		// fall back to older ones, so try older versions first.
		ignc2, report, err := v2.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV2 = &ignc2
			return nil
		} else if err != ignerr.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}

		ignc21, report, err := v21.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV21 = &ignc21
			return nil
		} else if err != ignerr.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}

		ignc22, report, err := v22.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV22 = &ignc22
			return nil
		} else if err != ignerr.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}

		ignc23, report, err := v23.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV23 = &ignc23
			return nil
		} else if err != ignerr.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}

		ignc24, report, err := v24.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV24 = &ignc24
			return nil
		} else if err != ignerr.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}

		ignc3, report3, err := v3.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV3 = &ignc3

			if ignv2 {
				newCfg, err := ignconverterv30tov22.Translate(*c.ignitionV3)
				if err != nil {
					return err
				}
				c.ignitionV22 = &newCfg
			}

			return nil
		} else if err != ign3err.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report3)
			return err
		}

		ignc31, report31, err := v31.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV31 = &ignc31

			if ignv2 {
				newCfg, err := ignconverterv31tov24.Translate(*c.ignitionV31)
				if err != nil {
					return err
				}
				c.ignitionV24 = &newCfg
			}

			return nil
		} else if err != ign3err.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report31)
			return err
		}

		ignc32, report32, err := v32.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV32 = &ignc32

			if ignv2 {
				newCfg, err := ignconverterv32tov24.Translate(*c.ignitionV32)
				if err != nil {
					return err
				}
				c.ignitionV24 = &newCfg
			}

			return nil
		} else if err != ign3err.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report32)
			return err
		}

		ignc33exp, report33exp, err := v33exp.Parse([]byte(u.data))
		if err == nil {
			c.ignitionV33exp = &ignc33exp
			if ignv2 {
				return fmt.Errorf("cannot convert Ignition from v3.3-experimental to v2")
			}
			return nil
		} else if err != ign3err.ErrUnknownVersion {
			plog.Errorf("invalid userdata: %v", report33exp)
			return err
		}

		// give up
		return err
	}

	switch u.kind {
	case kindEmpty:
		// empty, noop
	case kindIgnition:
		err := renderIgnition()
		if err != nil {
			return nil, err
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

		c.ignitionV22 = &ignc
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
	if c.ignitionV2 != nil {
		buf, _ := json.Marshal(c.ignitionV2)
		return string(buf)
	} else if c.ignitionV21 != nil {
		buf, _ := json.Marshal(c.ignitionV21)
		return string(buf)
	} else if c.ignitionV22 != nil {
		buf, _ := json.Marshal(c.ignitionV22)
		return string(buf)
	} else if c.ignitionV23 != nil {
		buf, _ := json.Marshal(c.ignitionV23)
		return string(buf)
	} else if c.ignitionV24 != nil {
		buf, _ := json.Marshal(c.ignitionV24)
		return string(buf)
	} else if c.ignitionV3 != nil {
		buf, _ := json.Marshal(c.ignitionV3)
		return string(buf)
	} else if c.ignitionV31 != nil {
		buf, _ := json.Marshal(c.ignitionV31)
		return string(buf)
	} else if c.ignitionV32 != nil {
		buf, _ := json.Marshal(c.ignitionV32)
		return string(buf)
	} else if c.ignitionV33exp != nil {
		buf, _ := json.Marshal(c.ignitionV33exp)
		return string(buf)
	}

	return ""
}

// MergeV3 merges a config with the ignitionV3 config via Ignition's merging function.
func (c *Conf) MergeV3(newConfig v3types.Config) {
	mergeConfig := v3.Merge(*c.ignitionV3, newConfig)
	c.ignitionV3 = &mergeConfig
}

// MergeV31 merges a config with the ignitionV31 config via Ignition's merging function.
func (c *Conf) MergeV31(newConfig v31types.Config) {
	mergeConfig := v31.Merge(*c.ignitionV31, newConfig)
	c.ignitionV31 = &mergeConfig
}

// MergeV32 merges a config with the ignitionV32 config via Ignition's merging function.
func (c *Conf) MergeV32(newConfig v32types.Config) {
	mergeConfig := v32.Merge(*c.ignitionV32, newConfig)
	c.ignitionV32 = &mergeConfig
}

// MergeV33exp merges a config with the ignitionV33exp config via Ignition's merging function.
func (c *Conf) MergeV33exp(newConfig v33exptypes.Config) {
	mergeConfig := v33exp.Merge(*c.ignitionV33exp, newConfig)
	c.ignitionV33exp = &mergeConfig
}

func (c *Conf) ValidConfig() bool {
	if !c.IsIgnition() {
		return false
	}
	val := c.getIgnitionValidateValue()
	if c.ignitionV3 != nil {
		rpt := ign3validate.ValidateWithContext(c.ignitionV3, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV31 != nil {
		rpt := ign3validate.ValidateWithContext(c.ignitionV31, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV32 != nil {
		rpt := ign3validate.ValidateWithContext(c.ignitionV32, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV33exp != nil {
		rpt := ign3validate.ValidateWithContext(c.ignitionV33exp, nil)
		return !rpt.IsFatal()
	} else {
		rpt := ignvalidate.ValidateWithoutSource(val)
		return !rpt.IsFatal()
	}
}

func (c *Conf) getIgnitionValidateValue() reflect.Value {
	if c.ignitionV2 != nil {
		return reflect.ValueOf(c.ignitionV2)
	} else if c.ignitionV21 != nil {
		return reflect.ValueOf(c.ignitionV21)
	} else if c.ignitionV22 != nil {
		return reflect.ValueOf(c.ignitionV22)
	} else if c.ignitionV23 != nil {
		return reflect.ValueOf(c.ignitionV23)
	} else if c.ignitionV24 != nil {
		return reflect.ValueOf(c.ignitionV24)
	} else if c.ignitionV3 != nil {
		return reflect.ValueOf(c.ignitionV3)
	} else if c.ignitionV31 != nil {
		return reflect.ValueOf(c.ignitionV31)
	} else if c.ignitionV32 != nil {
		return reflect.ValueOf(c.ignitionV32)
	} else if c.ignitionV33exp != nil {
		return reflect.ValueOf(c.ignitionV33exp)
	}
	return reflect.ValueOf(nil)
}

// WriteFile writes the userdata in Conf to a local file.
func (c *Conf) WriteFile(name string) error {
	return ioutil.WriteFile(name, []byte(c.String()), 0666)
}

// Bytes returns the serialized userdata in Conf.
func (c *Conf) Bytes() []byte {
	return []byte(c.String())
}

func (c *Conf) addFileV2(path, filesystem, contents string, mode int) {
	u, err := url.Parse(dataurl.EncodeBytes([]byte(contents)))
	if err != nil {
		plog.Warningf("parsing dataurl contents: %v", err)
		return
	}
	c.ignitionV2.Storage.Files = append(c.ignitionV2.Storage.Files, v2types.File{
		Filesystem: filesystem,
		Path:       v2types.Path(path),
		Contents: v2types.FileContents{
			Source: v2types.Url(*u),
		},
		Mode: v2types.FileMode(os.FileMode(mode)),
	})
}

func (c *Conf) addFileV21(path, filesystem, contents string, mode int) {
	c.ignitionV21.Storage.Files = append(c.ignitionV21.Storage.Files, v21types.File{
		Node: v21types.Node{
			Filesystem: filesystem,
			Path:       path,
		},
		FileEmbedded1: v21types.FileEmbedded1{
			Contents: v21types.FileContents{
				Source: dataurl.EncodeBytes([]byte(contents)),
			},
			Mode: mode,
		},
	})
}

func (c *Conf) addFileV22(path, filesystem, contents string, mode int) {
	c.ignitionV22.Storage.Files = append(c.ignitionV22.Storage.Files, v22types.File{
		Node: v22types.Node{
			Filesystem: filesystem,
			Path:       path,
		},
		FileEmbedded1: v22types.FileEmbedded1{
			Contents: v22types.FileContents{
				Source: dataurl.EncodeBytes([]byte(contents)),
			},
			Mode: &mode,
		},
	})
}

func (c *Conf) addFileV23(path, filesystem, contents string, mode int) {
	c.ignitionV23.Storage.Files = append(c.ignitionV23.Storage.Files, v23types.File{
		Node: v23types.Node{
			Filesystem: filesystem,
			Path:       path,
		},
		FileEmbedded1: v23types.FileEmbedded1{
			Contents: v23types.FileContents{
				Source: dataurl.EncodeBytes([]byte(contents)),
			},
			Mode: &mode,
		},
	})
}

func (c *Conf) addFileV24(path, filesystem, contents string, mode int) {
	c.ignitionV24.Storage.Files = append(c.ignitionV24.Storage.Files, v24types.File{
		Node: v24types.Node{
			Filesystem: filesystem,
			Path:       path,
		},
		FileEmbedded1: v24types.FileEmbedded1{
			Contents: v24types.FileContents{
				Source: dataurl.EncodeBytes([]byte(contents)),
			},
			Mode: &mode,
		},
	})
}

func (c *Conf) addFileV3(path, filesystem, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Storage: v3types.Storage{
			Files: []v3types.File{
				{
					Node: v3types.Node{
						Path: path,
					},
					FileEmbedded1: v3types.FileEmbedded1{
						Contents: v3types.FileContents{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV3(newConfig)
}

func (c *Conf) addFileV31(path, filesystem, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v31types.Config{
		Ignition: v31types.Ignition{
			Version: "3.1.0",
		},
		Storage: v31types.Storage{
			Files: []v31types.File{
				{
					Node: v31types.Node{
						Path: path,
					},
					FileEmbedded1: v31types.FileEmbedded1{
						Contents: v31types.Resource{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV31(newConfig)
}

func (c *Conf) addFileV32(path, filesystem, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v32types.Config{
		Ignition: v32types.Ignition{
			Version: "3.2.0",
		},
		Storage: v32types.Storage{
			Files: []v32types.File{
				{
					Node: v32types.Node{
						Path: path,
					},
					FileEmbedded1: v32types.FileEmbedded1{
						Contents: v32types.Resource{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV32(newConfig)
}

func (c *Conf) addFileV33exp(path, filesystem, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v33exptypes.Config{
		Ignition: v33exptypes.Ignition{
			Version: "3.3.0-experimental",
		},
		Storage: v33exptypes.Storage{
			Files: []v33exptypes.File{
				{
					Node: v33exptypes.Node{
						Path: path,
					},
					FileEmbedded1: v33exptypes.FileEmbedded1{
						Contents: v33exptypes.Resource{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV33exp(newConfig)
}

func (c *Conf) AddFile(path, filesystem, contents string, mode int) {
	if c.ignitionV3 != nil {
		c.addFileV3(path, filesystem, contents, mode)
	} else if c.ignitionV31 != nil {
		c.addFileV31(path, filesystem, contents, mode)
	} else if c.ignitionV32 != nil {
		c.addFileV32(path, filesystem, contents, mode)
	} else if c.ignitionV33exp != nil {
		c.addFileV33exp(path, filesystem, contents, mode)
	} else if c.ignitionV2 != nil {
		c.addFileV2(path, filesystem, contents, mode)
	} else if c.ignitionV21 != nil {
		c.addFileV21(path, filesystem, contents, mode)
	} else if c.ignitionV22 != nil {
		c.addFileV22(path, filesystem, contents, mode)
	} else if c.ignitionV23 != nil {
		c.addFileV23(path, filesystem, contents, mode)
	} else if c.ignitionV24 != nil {
		c.addFileV24(path, filesystem, contents, mode)
	} else {
		panic("Could not find a supported Ignition config version")
	}
}

func (c *Conf) addSystemdUnitV2(name, contents string, enable, mask bool) {
	c.ignitionV2.Systemd.Units = append(c.ignitionV2.Systemd.Units, v2types.SystemdUnit{
		Name:     v2types.SystemdUnitName(name),
		Contents: contents,
		Enable:   enable,
		Mask:     mask,
	})
}

func (c *Conf) addSystemdUnitV21(name, contents string, enable, mask bool) {
	c.ignitionV21.Systemd.Units = append(c.ignitionV21.Systemd.Units, v21types.Unit{
		Name:     name,
		Contents: contents,
		Enabled:  &enable,
		Mask:     mask,
	})
}

func (c *Conf) addSystemdUnitV22(name, contents string, enable, mask bool) {
	c.ignitionV22.Systemd.Units = append(c.ignitionV22.Systemd.Units, v22types.Unit{
		Name:     name,
		Contents: contents,
		Enabled:  &enable,
		Mask:     mask,
	})
}

func (c *Conf) addSystemdUnitV23(name, contents string, enable, mask bool) {
	c.ignitionV23.Systemd.Units = append(c.ignitionV23.Systemd.Units, v23types.Unit{
		Name:     name,
		Contents: contents,
		Enabled:  &enable,
		Mask:     mask,
	})
}

func (c *Conf) addSystemdUnitV24(name, contents string, enable, mask bool) {
	c.ignitionV24.Systemd.Units = append(c.ignitionV24.Systemd.Units, v24types.Unit{
		Name:     name,
		Contents: contents,
		Enabled:  &enable,
		Mask:     mask,
	})
}

func (c *Conf) addSystemdUnitV3(name, contents string, enable, mask bool) {
	newConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: v3types.Systemd{
			Units: []v3types.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV3(newConfig)
}

func (c *Conf) addSystemdUnitV31(name, contents string, enable, mask bool) {
	newConfig := v31types.Config{
		Ignition: v31types.Ignition{
			Version: "3.1.0",
		},
		Systemd: v31types.Systemd{
			Units: []v31types.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV31(newConfig)
}

func (c *Conf) addSystemdUnitV32(name, contents string, enable, mask bool) {
	newConfig := v32types.Config{
		Ignition: v32types.Ignition{
			Version: "3.2.0",
		},
		Systemd: v32types.Systemd{
			Units: []v32types.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV32(newConfig)
}

func (c *Conf) addSystemdUnitV33exp(name, contents string, enable, mask bool) {
	newConfig := v33exptypes.Config{
		Ignition: v33exptypes.Ignition{
			Version: "3.3.0-experimental",
		},
		Systemd: v33exptypes.Systemd{
			Units: []v33exptypes.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV33exp(newConfig)
}

func (c *Conf) AddSystemdUnit(name, contents string, state systemdUnitState) {
	enable, mask := false, false
	switch state {
	case Enable:
		enable = true
	case Mask:
		mask = true
	}
	if c.ignitionV2 != nil {
		c.addSystemdUnitV2(name, contents, enable, mask)
	} else if c.ignitionV21 != nil {
		c.addSystemdUnitV21(name, contents, enable, mask)
	} else if c.ignitionV22 != nil {
		c.addSystemdUnitV22(name, contents, enable, mask)
	} else if c.ignitionV23 != nil {
		c.addSystemdUnitV23(name, contents, enable, mask)
	} else if c.ignitionV24 != nil {
		c.addSystemdUnitV24(name, contents, enable, mask)
	} else if c.ignitionV3 != nil {
		c.addSystemdUnitV3(name, contents, enable, mask)
	} else if c.ignitionV31 != nil {
		c.addSystemdUnitV31(name, contents, enable, mask)
	} else if c.ignitionV32 != nil {
		c.addSystemdUnitV32(name, contents, enable, mask)
	} else if c.ignitionV33exp != nil {
		c.addSystemdUnitV33exp(name, contents, enable, mask)
	} else {
		panic("Could not find a supported Ignition config version")
	}
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

func (c *Conf) addSystemdDropinV22(service, name, contents string) {
	for i, unit := range c.ignitionV22.Systemd.Units {
		if unit.Name == service {
			unit.Dropins = append(unit.Dropins, v22types.SystemdDropin{
				Name:     name,
				Contents: contents,
			})
			c.ignitionV22.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV22.Systemd.Units = append(c.ignitionV22.Systemd.Units, v22types.Unit{
		Name: service,
		Dropins: []v22types.SystemdDropin{
			{
				Name:     name,
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinV23(service, name, contents string) {
	for i, unit := range c.ignitionV23.Systemd.Units {
		if unit.Name == service {
			unit.Dropins = append(unit.Dropins, v23types.SystemdDropin{
				Name:     name,
				Contents: contents,
			})
			c.ignitionV23.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV23.Systemd.Units = append(c.ignitionV23.Systemd.Units, v23types.Unit{
		Name: service,
		Dropins: []v23types.SystemdDropin{
			{
				Name:     name,
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinV24(service, name, contents string) {
	for i, unit := range c.ignitionV24.Systemd.Units {
		if unit.Name == service {
			unit.Dropins = append(unit.Dropins, v24types.SystemdDropin{
				Name:     name,
				Contents: contents,
			})
			c.ignitionV24.Systemd.Units[i] = unit
			return
		}
	}
	c.ignitionV24.Systemd.Units = append(c.ignitionV24.Systemd.Units, v24types.Unit{
		Name: service,
		Dropins: []v24types.SystemdDropin{
			{
				Name:     name,
				Contents: contents,
			},
		},
	})
}

func (c *Conf) addSystemdDropinV3(service, name, contents string) {
	newConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: v3types.Systemd{
			Units: []v3types.Unit{
				{
					Name: service,
					Dropins: []v3types.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV3(newConfig)
}

func (c *Conf) addSystemdDropinV31(service, name, contents string) {
	newConfig := v31types.Config{
		Ignition: v31types.Ignition{
			Version: "3.1.0",
		},
		Systemd: v31types.Systemd{
			Units: []v31types.Unit{
				{
					Name: service,
					Dropins: []v31types.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV31(newConfig)
}

func (c *Conf) addSystemdDropinV32(service, name, contents string) {
	newConfig := v32types.Config{
		Ignition: v32types.Ignition{
			Version: "3.2.0",
		},
		Systemd: v32types.Systemd{
			Units: []v32types.Unit{
				{
					Name: service,
					Dropins: []v32types.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV32(newConfig)
}

func (c *Conf) addSystemdDropinV33exp(service, name, contents string) {
	newConfig := v33exptypes.Config{
		Ignition: v33exptypes.Ignition{
			Version: "3.3.0-experimental",
		},
		Systemd: v33exptypes.Systemd{
			Units: []v33exptypes.Unit{
				{
					Name: service,
					Dropins: []v33exptypes.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV33exp(newConfig)
}

func (c *Conf) AddSystemdUnitDropin(service, name, contents string) {
	if c.ignitionV2 != nil {
		c.addSystemdDropinV2(service, name, contents)
	} else if c.ignitionV21 != nil {
		c.addSystemdDropinV21(service, name, contents)
	} else if c.ignitionV22 != nil {
		c.addSystemdDropinV22(service, name, contents)
	} else if c.ignitionV23 != nil {
		c.addSystemdDropinV23(service, name, contents)
	} else if c.ignitionV24 != nil {
		c.addSystemdDropinV24(service, name, contents)
	} else if c.ignitionV3 != nil {
		c.addSystemdDropinV3(service, name, contents)
	} else if c.ignitionV31 != nil {
		c.addSystemdDropinV31(service, name, contents)
	} else if c.ignitionV32 != nil {
		c.addSystemdDropinV32(service, name, contents)
	} else if c.ignitionV33exp != nil {
		c.addSystemdDropinV33exp(service, name, contents)
	} else {
		panic("Could not find a supported Ignition config version")
	}
}

func (c *Conf) addAuthorizedKeysV2(username string, keys []string) {
	for i := range c.ignitionV2.Passwd.Users {
		user := &c.ignitionV2.Passwd.Users[i]
		if user.Name == username {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keys...)
			return
		}
	}
	c.ignitionV2.Passwd.Users = append(c.ignitionV2.Passwd.Users, v2types.User{
		Name:              username,
		SSHAuthorizedKeys: keys,
	})
}

func (c *Conf) addAuthorizedKeysV21(username string, keys []string) {
	var keyObjs []v21types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v21types.SSHAuthorizedKey(key))
	}
	for i := range c.ignitionV21.Passwd.Users {
		user := &c.ignitionV21.Passwd.Users[i]
		if user.Name == username {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyObjs...)
			return
		}
	}
	c.ignitionV21.Passwd.Users = append(c.ignitionV21.Passwd.Users, v21types.PasswdUser{
		Name:              username,
		SSHAuthorizedKeys: keyObjs,
	})
}

func (c *Conf) addAuthorizedKeysV22(username string, keys []string) {
	var keyObjs []v22types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v22types.SSHAuthorizedKey(key))
	}
	for i := range c.ignitionV22.Passwd.Users {
		user := &c.ignitionV22.Passwd.Users[i]
		if user.Name == username {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyObjs...)
			return
		}
	}
	c.ignitionV22.Passwd.Users = append(c.ignitionV22.Passwd.Users, v22types.PasswdUser{
		Name:              username,
		SSHAuthorizedKeys: keyObjs,
	})
}

func (c *Conf) addAuthorizedKeysV23(username string, keys []string) {
	var keyObjs []v23types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v23types.SSHAuthorizedKey(key))
	}
	for i := range c.ignitionV23.Passwd.Users {
		user := &c.ignitionV23.Passwd.Users[i]
		if user.Name == username {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyObjs...)
			return
		}
	}
	c.ignitionV23.Passwd.Users = append(c.ignitionV23.Passwd.Users, v23types.PasswdUser{
		Name:              username,
		SSHAuthorizedKeys: keyObjs,
	})
}

func (c *Conf) addAuthorizedKeysV24(username string, keys []string) {
	var keyObjs []v24types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v24types.SSHAuthorizedKey(key))
	}
	for i := range c.ignitionV24.Passwd.Users {
		user := &c.ignitionV24.Passwd.Users[i]
		if user.Name == username {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, keyObjs...)
			return
		}
	}
	c.ignitionV24.Passwd.Users = append(c.ignitionV24.Passwd.Users, v24types.PasswdUser{
		Name:              username,
		SSHAuthorizedKeys: keyObjs,
	})
}

func (c *Conf) addAuthorizedKeysV3(username string, keys []string) {
	var keyObjs []v3types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v3types.SSHAuthorizedKey(key))
	}
	newConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Passwd: v3types.Passwd{
			Users: []v3types.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV3(newConfig)
}

func (c *Conf) addAuthorizedKeysV31(username string, keys []string) {
	var keyObjs []v31types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v31types.SSHAuthorizedKey(key))
	}
	newConfig := v31types.Config{
		Ignition: v31types.Ignition{
			Version: "3.1.0",
		},
		Passwd: v31types.Passwd{
			Users: []v31types.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV31(newConfig)
}

func (c *Conf) addAuthorizedKeysV32(username string, keys []string) {
	var keyObjs []v32types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v32types.SSHAuthorizedKey(key))
	}
	newConfig := v32types.Config{
		Ignition: v32types.Ignition{
			Version: "3.2.0",
		},
		Passwd: v32types.Passwd{
			Users: []v32types.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV32(newConfig)
}

func (c *Conf) addAuthorizedKeysV33exp(username string, keys []string) {
	var keyObjs []v33exptypes.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v33exptypes.SSHAuthorizedKey(key))
	}
	newConfig := v33exptypes.Config{
		Ignition: v33exptypes.Ignition{
			Version: "3.3.0-experimental",
		},
		Passwd: v33exptypes.Passwd{
			Users: []v33exptypes.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV33exp(newConfig)
}

// AddAuthorizedKeys adds an Ignition config to add the given keys to the SSH
// authorized_keys file for the given user.
func (c *Conf) AddAuthorizedKeys(user string, keys []string) {
	if c.ignitionV2 != nil {
		c.addAuthorizedKeysV2(user, keys)
	} else if c.ignitionV21 != nil {
		c.addAuthorizedKeysV21(user, keys)
	} else if c.ignitionV22 != nil {
		c.addAuthorizedKeysV22(user, keys)
	} else if c.ignitionV23 != nil {
		c.addAuthorizedKeysV23(user, keys)
	} else if c.ignitionV24 != nil {
		c.addAuthorizedKeysV24(user, keys)
	} else if c.ignitionV3 != nil {
		c.addAuthorizedKeysV3(user, keys)
	} else if c.ignitionV31 != nil {
		c.addAuthorizedKeysV31(user, keys)
	} else if c.ignitionV32 != nil {
		c.addAuthorizedKeysV32(user, keys)
	} else if c.ignitionV33exp != nil {
		c.addAuthorizedKeysV33exp(user, keys)
	} else {
		panic("Could not find a supported Ignition config version")
	}
}

// CopyKeys copies public keys from agent ag into the configuration to the
// appropriate configuration section for the core user.
func (c *Conf) CopyKeys(keys []*agent.Key) {
	var keyStrs []string
	for _, key := range keys {
		keyStrs = append(keyStrs, key.String())
	}
	c.AddAuthorizedKeys("core", keyStrs)
}

func (c *Conf) addConfigSourceV2(source string) {
	url, err := url.Parse(source)
	if err != nil {
		panic(err)
	}
	c.ignitionV2.Ignition.Config.Append = append(c.ignitionV2.Ignition.Config.Append, v2types.ConfigReference{
		Source: v2types.Url(*url),
		Verification: v2types.Verification{
			Hash: nil,
		},
	})
}

func (c *Conf) addConfigSourceV21(source string) {
	c.ignitionV21.Ignition.Config.Append = append(c.ignitionV21.Ignition.Config.Append, v21types.ConfigReference{
		Source: source,
		Verification: v21types.Verification{
			Hash: nil,
		},
	})
}

func (c *Conf) addConfigSourceV22(source string) {
	c.ignitionV22.Ignition.Config.Append = append(c.ignitionV22.Ignition.Config.Append, v22types.ConfigReference{
		Source: source,
		Verification: v22types.Verification{
			Hash: nil,
		},
	})
}

func (c *Conf) addConfigSourceV23(source string) {
	c.ignitionV23.Ignition.Config.Append = append(c.ignitionV23.Ignition.Config.Append, v23types.ConfigReference{
		Source: source,
		Verification: v23types.Verification{
			Hash: nil,
		},
	})
}

func (c *Conf) addConfigSourceV24(source string) {
	var headers []v24types.HTTPHeader
	c.ignitionV24.Ignition.Config.Append = append(c.ignitionV24.Ignition.Config.Append, v24types.ConfigReference{
		HTTPHeaders: headers,
		Source:      source,
		Verification: v24types.Verification{
			Hash: nil,
		},
	})
}

func (c *Conf) addConfigSourceV3(source string) {
	newConfig := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
			Config: v3types.IgnitionConfig{
				Merge: []v3types.ConfigReference{
					v3types.ConfigReference{
						Source: &source,
					},
				},
			},
		},
	}
	c.MergeV3(newConfig)
}

func (c *Conf) addConfigSourceV31(source string) {
	var resources []v31types.Resource
	var headers []v31types.HTTPHeader
	resources = append(resources, v31types.Resource{
		Compression: nil,
		HTTPHeaders: headers,
		Source:      &source,
		Verification: v31types.Verification{
			Hash: nil,
		},
	})
	newConfig := v31types.Config{
		Ignition: v31types.Ignition{
			Version: "3.1.0",
			Config: v31types.IgnitionConfig{
				Merge: resources,
			},
		},
	}
	c.MergeV31(newConfig)
}

func (c *Conf) addConfigSourceV32(source string) {
	var resources []v32types.Resource
	var headers []v32types.HTTPHeader
	resources = append(resources, v32types.Resource{
		Compression: nil,
		HTTPHeaders: headers,
		Source:      &source,
		Verification: v32types.Verification{
			Hash: nil,
		},
	})
	newConfig := v32types.Config{
		Ignition: v32types.Ignition{
			Version: "3.2.0",
			Config: v32types.IgnitionConfig{
				Merge: resources,
			},
		},
	}
	c.MergeV32(newConfig)
}

func (c *Conf) addConfigSourceV33exp(source string) {
	var resources []v33exptypes.Resource
	var headers []v33exptypes.HTTPHeader
	resources = append(resources, v33exptypes.Resource{
		Compression: nil,
		HTTPHeaders: headers,
		Source:      &source,
		Verification: v33exptypes.Verification{
			Hash: nil,
		},
	})
	newConfig := v33exptypes.Config{
		Ignition: v33exptypes.Ignition{
			Version: "3.3.0-experimental",
			Config: v33exptypes.IgnitionConfig{
				Merge: resources,
			},
		},
	}
	c.MergeV33exp(newConfig)
}

// AddConfigSource adds an Ignition config to append (v2) or merge (v3) the
// config available at the `source` URL with the current config.
func (c *Conf) AddConfigSource(source string) {
	if c.ignitionV2 != nil {
		c.addConfigSourceV2(source)
	} else if c.ignitionV21 != nil {
		c.addConfigSourceV21(source)
	} else if c.ignitionV22 != nil {
		c.addConfigSourceV22(source)
	} else if c.ignitionV23 != nil {
		c.addConfigSourceV23(source)
	} else if c.ignitionV24 != nil {
		c.addConfigSourceV24(source)
	} else if c.ignitionV3 != nil {
		c.addConfigSourceV3(source)
	} else if c.ignitionV31 != nil {
		c.addConfigSourceV31(source)
	} else if c.ignitionV32 != nil {
		c.addConfigSourceV32(source)
	} else if c.ignitionV33exp != nil {
		c.addConfigSourceV33exp(source)
	} else {
		panic("Could not find a supported Ignition config version")
	}
}

// IsIgnition returns true if the config is for Ignition.
// Returns false in the case of empty configs
func (c *Conf) IsIgnition() bool {
	return c.ignitionV2 != nil || c.ignitionV21 != nil || c.ignitionV22 != nil || c.ignitionV23 != nil || c.ignitionV24 != nil || c.ignitionV3 != nil || c.ignitionV31 != nil || c.ignitionV32 != nil || c.ignitionV33exp != nil
}

func (c *Conf) IsEmpty() bool {
	return !c.IsIgnition()
}

func getAutologinUnit(name, args string) string {
	return fmt.Sprintf(`[Service]
	ExecStart=
	ExecStart=-/sbin/agetty --autologin core -o '-p -f core' %s %%I $TERM
	`, args)
}

// AddAutoLogin adds an Ignition config for automatic login on consoles
func (c *Conf) AddAutoLogin() {
	c.AddSystemdUnitDropin("getty@.service", "10-autologin.conf", getAutologinUnit("getty@.service", "--noclear"))
	c.AddSystemdUnitDropin("serial-getty@.service", "10-autologin.conf", getAutologinUnit("serial-getty@.service", "--keep-baud 115200,38400,9600"))
}

// Mount9p adds an Ignition config to mount an folder with 9p
func (c *Conf) Mount9p(dest string, readonly bool) {
	readonlyStr := ""
	if readonly {
		readonlyStr = ",ro"
	}
	content := fmt.Sprintf(`[Unit]
DefaultDependencies=no
After=systemd-tmpfiles-setup.service
Before=basic.target
[Mount]
What=%s
Where=%s
Type=9p
Options=trans=virtio,version=9p2000.L%s
[Install]
WantedBy=multi-user.target
`, dest, dest, readonlyStr)
	c.AddSystemdUnit(fmt.Sprintf("%s.mount", systemdunit.UnitNameEscape(dest[1:])), content, Enable)
}
