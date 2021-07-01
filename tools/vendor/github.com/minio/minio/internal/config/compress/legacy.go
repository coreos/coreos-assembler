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

package compress

import (
	"strings"

	"github.com/minio/minio/internal/config"
)

// Legacy envs.
const (
	EnvCompress                = "MINIO_COMPRESS"
	EnvCompressMimeTypesLegacy = "MINIO_COMPRESS_MIMETYPES"
)

// SetCompressionConfig - One time migration code needed, for migrating from older config to new for Compression.
func SetCompressionConfig(s config.Config, cfg Config) {
	if !cfg.Enabled {
		// No need to save disabled settings in new config.
		return
	}
	s[config.CompressionSubSys][config.Default] = config.KVS{
		config.KV{
			Key:   config.Enable,
			Value: config.EnableOn,
		},
		config.KV{
			Key:   Extensions,
			Value: strings.Join(cfg.Extensions, config.ValueSeparator),
		},
		config.KV{
			Key:   MimeTypes,
			Value: strings.Join(cfg.MimeTypes, config.ValueSeparator),
		},
	}
}
