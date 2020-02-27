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

package sdk

import (
	"github.com/pborman/uuid"
)

// Partition UUIDs for CoreOS systems.
var (
	USRAUUID = uuid.UUID{0x71, 0x30, 0xC9, 0x4A, 0x21, 0x3A, 0x4E, 0x5A, 0x8E, 0x26, 0x6C, 0xCE, 0x96, 0x62, 0xF1, 0x32}
	USRBUUID = uuid.UUID{0xE0, 0x3D, 0xD3, 0x5C, 0x7C, 0x2D, 0x4A, 0x47, 0xB3, 0xFE, 0x27, 0xF1, 0x57, 0x80, 0xA5, 0x7C}
)
