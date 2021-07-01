// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package config

// HelpKV - implements help messages for keys
// with value as description of the keys.
type HelpKV struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`

	// Indicates if the value contains sensitive info
	// that shouldn't be exposed in certain apis
	Sensitive bool `json:"-"`

	// Indicates if sub-sys supports multiple targets.
	MultipleTargets bool `json:"multipleTargets"`
}

// HelpKVS - implement order of keys help messages.
type HelpKVS []HelpKV

// Lookup - lookup a key from help kvs.
func (hkvs HelpKVS) Lookup(key string) (HelpKV, bool) {
	for _, hkv := range hkvs {
		if hkv.Key == key {
			return hkv, true
		}
	}
	return HelpKV{}, false
}

// DefaultComment used across all sub-systems.
const DefaultComment = "optionally add a comment to this setting"

// Region and Worm help is documented in default config
var (
	RegionHelp = HelpKVS{
		HelpKV{
			Key:         RegionName,
			Type:        "string",
			Description: `name of the location of the server e.g. "us-west-rack2"`,
			Optional:    true,
		},
		HelpKV{
			Key:         Comment,
			Type:        "sentence",
			Description: DefaultComment,
			Optional:    true,
		},
	}
)
