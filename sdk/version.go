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

package sdk

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	coreosId = "{E96281A6-D1AF-4BDE-9A0A-97B76E56DC57}"
)

func GetVersion(dir string) (ver string, err error) {
	const key = "COREOS_VERSION="

	f, err := os.Open(filepath.Join(dir, "version.txt"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, key) {
			ver = line[len(key):]
			break
		}
	}
	err = scanner.Err()

	if err == nil && ver == "" {
		err = fmt.Errorf("Missing %s value in %s", key, f.Name())
	}

	return
}

func GetLatestVersion() (string, error) {
	return GetVersion(BuildImageDir("latest"))
}

func GetDefaultAppId() string {
	// This is a function in case the id needs to be configurable.
	return coreosId
}
