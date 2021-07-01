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

// +build fips

package hash

import (
	"crypto/sha256"
	"hash"
)

// newSHA256 returns a new hash.Hash computing the SHA256 checksum.
// The SHA256 implementation is FIPS 140-2 compliant when the
// boringcrypto branch of Go is used.
// Ref: https://github.com/golang/go/tree/dev.boringcrypto
func newSHA256() hash.Hash { return sha256.New() }
