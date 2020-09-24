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

//go:generate protoc --go_out=import_path=$GOPACKAGE:. update_metadata.proto

package metadata

// Magic is the first four bytes of any update payload.
const Magic = "CrAU"

// Major version of the payload format.
const Version = 1

// DeltaArchiveHeader begins the payload file.
type DeltaArchiveHeader struct {
	Magic        [4]byte // "CrAU"
	Version      uint64  // 1
	ManifestSize uint64
}
