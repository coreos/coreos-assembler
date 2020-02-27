// Copyright 2015 CoreOS, Inc.
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

package local

import (
	"os"
	"path"

	"github.com/coreos/mantle/platform/conf"
)

// MakeConfigDrive creates a config drive directory tree under outputDir
// and returns the path to the top level directory.
func MakeConfigDrive(userdata *conf.Conf, outputDir string) (string, error) {
	drivePath := path.Join(outputDir, "config-2")
	userPath := path.Join(drivePath, "openstack/latest/user_data")

	if err := os.MkdirAll(path.Dir(userPath), 0777); err != nil {
		os.RemoveAll(drivePath)
		return "", err
	}

	if err := userdata.WriteFile(userPath); err != nil {
		os.RemoveAll(drivePath)
		return "", err
	}

	return drivePath, nil
}
