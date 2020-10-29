// Copyright 2017-18 Daniel Swarbrick. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package drivedb

import (
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

// SMART attribute conversion rule
type AttrConv struct {
	Conv string `yaml:"conv"`
	Name string `yaml:"name"`
}

type DriveModel struct {
	Family         string              `yaml:"family"`
	ModelRegex     string              `yaml:"model_regex"`
	FirmwareRegex  string              `yaml:"firmware_regex"`
	WarningMsg     string              `yaml:"warning"`
	Presets        map[string]AttrConv `yaml:"presets"`
	CompiledRegexp *regexp.Regexp
}

type DriveDb struct {
	Drives []DriveModel `yaml:"drives"`
}

// LookupDrive returns the most appropriate DriveModel for a given ATA IDENTIFY value.
func (db *DriveDb) LookupDrive(ident []byte) DriveModel {
	var model DriveModel

	for _, d := range db.Drives {
		// Skip placeholder entry
		if strings.HasPrefix(d.Family, "$Id") {
			continue
		}

		if d.Family == "DEFAULT" {
			model = d
			continue
		}

		if d.CompiledRegexp.Match(ident) {
			model.Family = d.Family
			model.ModelRegex = d.ModelRegex
			model.FirmwareRegex = d.FirmwareRegex
			model.WarningMsg = d.WarningMsg
			model.CompiledRegexp = d.CompiledRegexp

			for id, p := range d.Presets {
				if _, exists := model.Presets[id]; exists {
					// Some drives override the conv but don't specify a name, so copy it from default
					if p.Name == "" {
						p.Name = model.Presets[id].Name
					}
				}
				model.Presets[id] = AttrConv{Name: p.Name, Conv: p.Conv}
			}

			break
		}
	}

	return model
}

// OpenDriveDb opens a YAML-formatted drive database, unmarshalls it, and returns a DriveDb.
func OpenDriveDb(dbfile string) (DriveDb, error) {
	var db DriveDb

	f, err := os.Open(dbfile)
	if err != nil {
		return db, nil
	}

	defer f.Close()
	dec := yaml.NewDecoder(f)

	if err := dec.Decode(&db); err != nil {
		return db, err
	}

	for i, d := range db.Drives {
		db.Drives[i].CompiledRegexp, _ = regexp.Compile(d.ModelRegex)
	}

	return db, nil
}
