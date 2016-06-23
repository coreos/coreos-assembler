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

package generator

import (
	"crypto/sha256"
	"io"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/golang/protobuf/proto"

	"github.com/coreos/mantle/update/metadata"
)

func NewInstallInfo(r io.ReadSeeker) (*metadata.InstallInfo, error) {
	sha := sha256.New()
	size, err := io.Copy(sha, r)
	if err != nil {
		return nil, err
	}

	if _, err := r.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}

	return &metadata.InstallInfo{
		Hash: sha.Sum(nil),
		Size: proto.Uint64(uint64(size)),
	}, nil
}
