// This file is part of MinIO Console Server
// Copyright (c) 2021 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package token

import (
	"strconv"

	"github.com/minio/console/pkg/auth/utils"
	"github.com/minio/pkg/env"
)

// ConsoleSTSDurationSeconds returns the default session duration for the STS requested tokens.
func GetConsoleSTSDurationInSeconds() int {
	duration, err := strconv.Atoi(env.Get(ConsoleSTSDurationSeconds, "3600"))
	if err != nil {
		duration = 3600
	}
	return duration
}

var defaultPBKDFPassphrase = utils.RandomCharString(64)

// GetPBKDFPassphrase returns passphrase for the pbkdf2 function used to encrypt JWT payload
func GetPBKDFPassphrase() string {
	return env.Get(ConsolePBKDFPassphrase, defaultPBKDFPassphrase)
}

var defaultPBKDFSalt = utils.RandomCharString(64)

// GetPBKDFSalt returns salt for the pbkdf2 function used to encrypt JWT payload
func GetPBKDFSalt() string {
	return env.Get(ConsolePBKDFSalt, defaultPBKDFSalt)
}
