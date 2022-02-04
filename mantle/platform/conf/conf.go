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
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	butane "github.com/coreos/butane/config"
	butaneCommon "github.com/coreos/butane/config/common"
	"github.com/coreos/go-semver/semver"
	systemdunit "github.com/coreos/go-systemd/unit"
	ignerr "github.com/coreos/ignition/v2/config/shared/errors"
	ignutil "github.com/coreos/ignition/v2/config/util"
	v3 "github.com/coreos/ignition/v2/config/v3_0"
	v3types "github.com/coreos/ignition/v2/config/v3_0/types"
	v31 "github.com/coreos/ignition/v2/config/v3_1"
	v31types "github.com/coreos/ignition/v2/config/v3_1/types"
	v32 "github.com/coreos/ignition/v2/config/v3_2"
	v32types "github.com/coreos/ignition/v2/config/v3_2/types"
	v33 "github.com/coreos/ignition/v2/config/v3_3"
	v33types "github.com/coreos/ignition/v2/config/v3_3/types"
	v34exp "github.com/coreos/ignition/v2/config/v3_4_experimental"
	v34exptypes "github.com/coreos/ignition/v2/config/v3_4_experimental/types"
	"github.com/coreos/ignition/v2/config/validate"
	"github.com/coreos/pkg/capnslog"
	"github.com/coreos/vcontext/report"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/crypto/ssh/agent"
)

type kind int

const (
	kindEmpty kind = iota
	kindIgnition
	kindButane
)

type systemdUnitState int

const (
	NoState systemdUnitState = iota
	Enable
	Mask
)

type WarningsAction int

const (
	IgnoreWarnings WarningsAction = iota
	ReportWarnings
	FailWarnings
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/conf")

// UserData is an immutable, unvalidated configuration for a CoreOS
// machine.
type UserData struct {
	kind      kind
	data      string
	extraKeys []*agent.Key // SSH keys to be injected during rendering
}

// Conf is a configuration for a CoreOS machine. Only Ignition spec 3 and later
// are supported.
type Conf struct {
	ignitionV3  *v3types.Config
	ignitionV31 *v31types.Config
	ignitionV32 *v32types.Config
	ignitionV33 *v33types.Config

	ignitionV34exp *v34exptypes.Config
}

// Empty creates a completely empty configuration. Any configuration addition
// applied to an empty config is ignored.
func Empty() *UserData {
	return &UserData{
		kind: kindEmpty,
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
		ignver, ok := os.LookupEnv("COSA_IGNITION_DEFAULT_VERSION")
		if !ok {
			ignver = "3.2.0"
		}
		data = fmt.Sprintf(`{"ignition": {"version": "%s"}}`, ignver)
	}
	return &UserData{
		kind: kindIgnition,
		data: data,
	}
}

// Butane returns a Butane UserData struct from the provided string.
func Butane(data string) *UserData {
	return &UserData{
		kind: kindButane,
		data: data,
	}
}

func Unknown(data string) *UserData {
	u := &UserData{
		data: data,
	}

	// Try to parse the config only to detect if we were provided an empty one
	_, _, err := v3.Parse([]byte(data))
	switch err {
	case ignerr.ErrEmpty:
		u.kind = kindEmpty
	default:
		// Guess whether this is an Ignition or Butane config.
		// This treats an invalid Ignition config as a Butane
		// config, and a Butane config in the JSON subset of YAML as
		// an Ignition config.
		var decoded interface{}
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			u.kind = kindButane
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

// Render parses userdata and returns a new Conf. It returns an error if the
// userdata can't be parsed, or if FailWarnings is selected and there are
// warnings.
func (u *UserData) Render(warnings WarningsAction) (*Conf, error) {
	c := &Conf{}

	handleWarnings := func(r report.Report) error {
		if len(r.Entries) > 0 {
			switch warnings {
			case IgnoreWarnings:
			case ReportWarnings:
				plog.Warningf("warnings parsing config: %s", r)
			case FailWarnings:
				plog.Errorf("warnings parsing config: %s", r)
				return errors.New("configured to treate config warnings as fatal")
			}
		}
		return nil
	}
	renderIgnition := func(data []byte) error {
		ver, report, err := ignutil.GetConfigVersion(data)
		if err != nil {
			plog.Errorf("invalid userdata: %v", report)
			return err
		}
		// We can't use ParseCompatibleVersion because that'll
		// upconvert older configs.
		switch ver {
		case semver.Version{Major: 3, Minor: 0}:
			ignc, report, err := v3.Parse(data)
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return err
			}
			c.ignitionV3 = &ignc
		case semver.Version{Major: 3, Minor: 1}:
			ignc, report, err := v31.Parse(data)
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return err
			}
			c.ignitionV31 = &ignc
		case semver.Version{Major: 3, Minor: 2}:
			ignc, report, err := v32.Parse(data)
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return err
			}
			c.ignitionV32 = &ignc
		case semver.Version{Major: 3, Minor: 3}:
			ignc, report, err := v33.Parse(data)
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return err
			}
			c.ignitionV33 = &ignc
		case semver.Version{Major: 3, Minor: 4, PreRelease: "experimental"}:
			ignc, report, err := v34exp.Parse(data)
			if err != nil {
				plog.Errorf("invalid userdata: %v", report)
				return err
			}
			c.ignitionV34exp = &ignc
		// Special case for the next stable version: wrap it in a
		// config of the current stable version, so we can still add
		// our config fragments without understanding the specified
		// config.  This path makes it easier to get a spec
		// stabilization through CI in the presence of external
		// tests using the experimental spec, since CI only needs to
		// ensure that the installed Ignition can parse the config,
		// not that Mantle can also parse it.
		case semver.Version{Major: 3, Minor: 4}:
			plog.Warningf("Generating wrapper for unsupported config spec")
			url, err := makeGzipDataUrl(data)
			if err != nil {
				return fmt.Errorf("generating data URL: %w", err)
			}
			c.ignitionV33 = &v33types.Config{
				Ignition: v33types.Ignition{
					Version: "3.3.0",
					Config: v33types.IgnitionConfig{
						Merge: []v33types.Resource{
							{
								Source:      ignutil.StrToPtr(url),
								Compression: ignutil.StrToPtr("gzip"),
							},
						},
					},
				},
			}
		default:
			return ignerr.ErrUnknownVersion
		}
		return handleWarnings(report)
	}

	switch u.kind {
	case kindEmpty:
		// empty, noop
	case kindIgnition:
		err := renderIgnition([]byte(u.data))
		if err != nil {
			return nil, err
		}
	case kindButane:
		ignc, report, err := butane.TranslateBytes([]byte(u.data), butaneCommon.TranslateBytesOptions{
			// allow variant: openshift but don't generate a MachineConfig
			Raw: true,
		})
		if err != nil {
			return nil, err
		}
		err = handleWarnings(report)
		if err != nil {
			return nil, err
		}
		err = renderIgnition(ignc)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("invalid kind")
	}

	if len(u.extraKeys) > 0 {
		// not a no-op in the zero-key case
		c.CopyKeys(u.extraKeys)
	}

	return c, nil
}

// String returns the string representation of the userdata in Conf.
func (c *Conf) String() string {
	if c.ignitionV3 != nil {
		buf, _ := json.Marshal(c.ignitionV3)
		return string(buf)
	} else if c.ignitionV31 != nil {
		buf, _ := json.Marshal(c.ignitionV31)
		return string(buf)
	} else if c.ignitionV32 != nil {
		buf, _ := json.Marshal(c.ignitionV32)
		return string(buf)
	} else if c.ignitionV33 != nil {
		buf, _ := json.Marshal(c.ignitionV33)
		return string(buf)
	} else if c.ignitionV34exp != nil {
		buf, _ := json.Marshal(c.ignitionV34exp)
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

// MergeV33 merges a config with the ignitionV33 config via Ignition's merging function.
func (c *Conf) MergeV33(newConfig v33types.Config) {
	mergeConfig := v33.Merge(*c.ignitionV33, newConfig)
	c.ignitionV33 = &mergeConfig
}

// MergeV34exp merges a config with the ignitionV34exp config via Ignition's merging function.
func (c *Conf) MergeV34exp(newConfig v34exptypes.Config) {
	mergeConfig := v34exp.Merge(*c.ignitionV34exp, newConfig)
	c.ignitionV34exp = &mergeConfig
}

// Merge all configs into a V3.3 config
func MergeAllConfigs(confObjs []*Conf) (*UserData, error) {
	config := Conf{
		ignitionV33: &v33types.Config{
			Ignition: v33types.Ignition{
				Version: "3.3.0",
			},
		},
	}
	objectsToMerge := &config.ignitionV33.Ignition.Config.Merge
	for _, conf := range confObjs {
		ud := conf.String()
		url := dataurl.EncodeBytes([]byte(ud))
		obj := v33types.Resource{
			Source: &url,
		}
		*objectsToMerge = append(*objectsToMerge, obj)
	}

	userData := Ignition(config.String())
	return userData, nil
}

// Config is compressed and added to another via data url
func (c *Conf) MaybeCompress() (string, error) {
	// Compress config
	var buff bytes.Buffer
	config := c.String()
	writer, err := gzip.NewWriterLevel(&buff, gzip.BestCompression)
	defer writer.Close()
	if _, err := writer.Write([]byte(config)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	// Encode as data url and add to replace clause in new config
	url := dataurl.EncodeBytes(buff.Bytes())
	compressionAlgo := "gzip"
	newConfigToReplace := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
			Config: v33types.IgnitionConfig{
				Replace: v33types.Resource{
					Source:      &url,
					Compression: &compressionAlgo,
				},
			},
		},
	}

	wrapperConf := Conf{ignitionV33: &newConfigToReplace}
	// sanity checks
	if !wrapperConf.ValidConfig() {
		err = errors.New("MaybeCompress: new config not valid")
	}
	// Verify that the new config is smaller than the old one
	newConfig := wrapperConf.String()
	if len(newConfig) < len(config) {
		return newConfig, err
	}

	return config, err
}

func (c *Conf) ValidConfig() bool {
	if !c.IsIgnition() {
		return false
	}
	if c.ignitionV3 != nil {
		rpt := validate.ValidateWithContext(c.ignitionV3, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV31 != nil {
		rpt := validate.ValidateWithContext(c.ignitionV31, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV32 != nil {
		rpt := validate.ValidateWithContext(c.ignitionV32, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV33 != nil {
		rpt := validate.ValidateWithContext(c.ignitionV33, nil)
		return !rpt.IsFatal()
	} else if c.ignitionV34exp != nil {
		rpt := validate.ValidateWithContext(c.ignitionV34exp, nil)
		return !rpt.IsFatal()
	} else {
		return false
	}
}

// WriteFile writes the userdata in Conf to a local file.
func (c *Conf) WriteFile(name string) error {
	return ioutil.WriteFile(name, []byte(c.String()), 0666)
}

// Bytes returns the serialized userdata in Conf.
func (c *Conf) Bytes() []byte {
	return []byte(c.String())
}

func (c *Conf) addFileV3(path, contents string, mode int) {
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

func (c *Conf) addFileV31(path, contents string, mode int) {
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

func (c *Conf) addFileV32(path, contents string, mode int) {
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

func (c *Conf) addFileV33(path, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
		},
		Storage: v33types.Storage{
			Files: []v33types.File{
				{
					Node: v33types.Node{
						Path: path,
					},
					FileEmbedded1: v33types.FileEmbedded1{
						Contents: v33types.Resource{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV33(newConfig)
}

func (c *Conf) addFileV34exp(path, contents string, mode int) {
	source := dataurl.EncodeBytes([]byte(contents))
	newConfig := v34exptypes.Config{
		Ignition: v34exptypes.Ignition{
			Version: "3.4.0-experimental",
		},
		Storage: v34exptypes.Storage{
			Files: []v34exptypes.File{
				{
					Node: v34exptypes.Node{
						Path: path,
					},
					FileEmbedded1: v34exptypes.FileEmbedded1{
						Contents: v34exptypes.Resource{
							Source: &source,
						},
						Mode: &mode,
					},
				},
			},
		},
	}
	c.MergeV34exp(newConfig)
}

func (c *Conf) AddFile(path, contents string, mode int) {
	if c.ignitionV3 != nil {
		c.addFileV3(path, contents, mode)
	} else if c.ignitionV31 != nil {
		c.addFileV31(path, contents, mode)
	} else if c.ignitionV32 != nil {
		c.addFileV32(path, contents, mode)
	} else if c.ignitionV33 != nil {
		c.addFileV33(path, contents, mode)
	} else if c.ignitionV34exp != nil {
		c.addFileV34exp(path, contents, mode)
	}
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

func (c *Conf) addSystemdUnitV33(name, contents string, enable, mask bool) {
	newConfig := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
		},
		Systemd: v33types.Systemd{
			Units: []v33types.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV33(newConfig)
}

func (c *Conf) addSystemdUnitV34exp(name, contents string, enable, mask bool) {
	newConfig := v34exptypes.Config{
		Ignition: v34exptypes.Ignition{
			Version: "3.4.0-experimental",
		},
		Systemd: v34exptypes.Systemd{
			Units: []v34exptypes.Unit{
				{
					Name:     name,
					Contents: &contents,
					Enabled:  &enable,
					Mask:     &mask,
				},
			},
		},
	}
	c.MergeV34exp(newConfig)
}

func (c *Conf) AddSystemdUnit(name, contents string, state systemdUnitState) {
	enable, mask := false, false
	switch state {
	case Enable:
		enable = true
	case Mask:
		mask = true
	}
	if c.ignitionV3 != nil {
		c.addSystemdUnitV3(name, contents, enable, mask)
	} else if c.ignitionV31 != nil {
		c.addSystemdUnitV31(name, contents, enable, mask)
	} else if c.ignitionV32 != nil {
		c.addSystemdUnitV32(name, contents, enable, mask)
	} else if c.ignitionV33 != nil {
		c.addSystemdUnitV33(name, contents, enable, mask)
	} else if c.ignitionV34exp != nil {
		c.addSystemdUnitV34exp(name, contents, enable, mask)
	}
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

func (c *Conf) addSystemdDropinV33(service, name, contents string) {
	newConfig := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
		},
		Systemd: v33types.Systemd{
			Units: []v33types.Unit{
				{
					Name: service,
					Dropins: []v33types.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV33(newConfig)
}

func (c *Conf) addSystemdDropinV34exp(service, name, contents string) {
	newConfig := v34exptypes.Config{
		Ignition: v34exptypes.Ignition{
			Version: "3.4.0-experimental",
		},
		Systemd: v34exptypes.Systemd{
			Units: []v34exptypes.Unit{
				{
					Name: service,
					Dropins: []v34exptypes.Dropin{
						{
							Name:     name,
							Contents: &contents,
						},
					},
				},
			},
		},
	}
	c.MergeV34exp(newConfig)
}

func (c *Conf) AddSystemdUnitDropin(service, name, contents string) {
	if c.ignitionV3 != nil {
		c.addSystemdDropinV3(service, name, contents)
	} else if c.ignitionV31 != nil {
		c.addSystemdDropinV31(service, name, contents)
	} else if c.ignitionV32 != nil {
		c.addSystemdDropinV32(service, name, contents)
	} else if c.ignitionV33 != nil {
		c.addSystemdDropinV33(service, name, contents)
	} else if c.ignitionV34exp != nil {
		c.addSystemdDropinV34exp(service, name, contents)
	}
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

func (c *Conf) addAuthorizedKeysV33(username string, keys []string) {
	var keyObjs []v33types.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v33types.SSHAuthorizedKey(key))
	}
	newConfig := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
		},
		Passwd: v33types.Passwd{
			Users: []v33types.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV33(newConfig)
}

func (c *Conf) addAuthorizedKeysV34exp(username string, keys []string) {
	var keyObjs []v34exptypes.SSHAuthorizedKey
	for _, key := range keys {
		keyObjs = append(keyObjs, v34exptypes.SSHAuthorizedKey(key))
	}
	newConfig := v34exptypes.Config{
		Ignition: v34exptypes.Ignition{
			Version: "3.4.0-experimental",
		},
		Passwd: v34exptypes.Passwd{
			Users: []v34exptypes.PasswdUser{
				{
					Name:              username,
					SSHAuthorizedKeys: keyObjs,
				},
			},
		},
	}
	c.MergeV34exp(newConfig)
}

// AddAuthorizedKeys adds an Ignition config to add the given keys to the SSH
// authorized_keys file for the given user.
func (c *Conf) AddAuthorizedKeys(user string, keys []string) {
	if c.ignitionV3 != nil {
		c.addAuthorizedKeysV3(user, keys)
	} else if c.ignitionV31 != nil {
		c.addAuthorizedKeysV31(user, keys)
	} else if c.ignitionV32 != nil {
		c.addAuthorizedKeysV32(user, keys)
	} else if c.ignitionV33 != nil {
		c.addAuthorizedKeysV33(user, keys)
	} else if c.ignitionV34exp != nil {
		c.addAuthorizedKeysV34exp(user, keys)
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

func (c *Conf) addConfigSourceV33(source string) {
	var resources []v33types.Resource
	var headers []v33types.HTTPHeader
	resources = append(resources, v33types.Resource{
		Compression: nil,
		HTTPHeaders: headers,
		Source:      &source,
		Verification: v33types.Verification{
			Hash: nil,
		},
	})
	newConfig := v33types.Config{
		Ignition: v33types.Ignition{
			Version: "3.3.0",
			Config: v33types.IgnitionConfig{
				Merge: resources,
			},
		},
	}
	c.MergeV33(newConfig)
}

func (c *Conf) addConfigSourceV34exp(source string) {
	var resources []v34exptypes.Resource
	var headers []v34exptypes.HTTPHeader
	resources = append(resources, v34exptypes.Resource{
		Compression: nil,
		HTTPHeaders: headers,
		Source:      &source,
		Verification: v34exptypes.Verification{
			Hash: nil,
		},
	})
	newConfig := v34exptypes.Config{
		Ignition: v34exptypes.Ignition{
			Version: "3.4.0-experimental",
			Config: v34exptypes.IgnitionConfig{
				Merge: resources,
			},
		},
	}
	c.MergeV34exp(newConfig)
}

// AddConfigSource adds an Ignition config to merge (v3) the
// config available at the `source` URL with the current config.
func (c *Conf) AddConfigSource(source string) {
	if c.ignitionV3 != nil {
		c.addConfigSourceV3(source)
	} else if c.ignitionV31 != nil {
		c.addConfigSourceV31(source)
	} else if c.ignitionV32 != nil {
		c.addConfigSourceV32(source)
	} else if c.ignitionV33 != nil {
		c.addConfigSourceV33(source)
	} else if c.ignitionV34exp != nil {
		c.addConfigSourceV34exp(source)
	}
}

// IsIgnition returns true if the config is for Ignition.
// Returns false in the case of empty configs
func (c *Conf) IsIgnition() bool {
	return c.ignitionV3 != nil || c.ignitionV31 != nil || c.ignitionV32 != nil || c.ignitionV33 != nil || c.ignitionV34exp != nil
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

// AddAutoResize adds an Ignition config for a `resize` function to resize serial consoles
func (c *Conf) AddAutoResize() {
	c.AddFile("/etc/profile.d/autoresize.sh", `
# adapted from https://wiki.archlinux.org/title/Working_with_the_serial_console#Resizing_a_terminal
resize_terminal() {
	local IFS='[;' escape geometry x y
	echo -en '\e7\e[r\e[999;999H\e[6n\e8'
	read -sd R escape geometry
	x=${geometry##*;} y=${geometry%%;*}
	if [[ ${COLUMNS} -ne ${x} || ${LINES} -ne ${y} ]];then
		stty cols ${x} rows ${y}
	fi
}
PROMPT_COMMAND+=(resize_terminal)`, 0644)
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
Options=trans=virtio,version=9p2000.L%s,msize=10485760
[Install]
WantedBy=multi-user.target
`, dest, dest, readonlyStr)
	c.AddSystemdUnit(fmt.Sprintf("%s.mount", systemdunit.UnitNameEscape(dest[1:])), content, Enable)
}

func makeGzipDataUrl(data []byte) (string, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, 9)
	if err != nil {
		return "", err
	}
	_, err = gz.Write(data)
	if err != nil {
		return "", err
	}
	err = gz.Close()
	if err != nil {
		return "", err
	}
	url := "data:;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	return url, nil
}
