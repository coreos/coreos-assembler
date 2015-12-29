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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	coreosId = "{E96281A6-D1AF-4BDE-9A0A-97B76E56DC57}"
)

func getVersion(dir, key string) (ver string, err error) {
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

func GetVersionFromDir(dir string) (string, error) {
	return getVersion(dir, "COREOS_VERSION=")
}

func GetLatestVersion() (string, error) {
	return getVersion(BuildImageDir("latest"), "COREOS_VERSION=")
}

func GetSDKVersion() (string, error) {
	return getVersion(filepath.Join(RepoRoot(), ".repo", "manifests"), "COREOS_SDK_VERSION=")
}

func GetSDKVersionFromDir(dir string) (string, error) {
	return getVersion(dir, "COREOS_SDK_VERSION=")
}

func GetSDKVersionFromRemoteRepo(url, branch string) (string, error) {
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	clone := exec.Command("git", "clone", "-q", "--depth=1", "--single-branch", "-b", branch, url, tmp)
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return "", err
	}

	return GetSDKVersionFromDir(tmp)
}

func GetDefaultAppId() string {
	// This is a function in case the id needs to be configurable.
	return coreosId
}

const (
	CoreOSEpoch = 1372636800
)

// GetCoreOSAge returns the number of days since the CoreOS epoch.
func GetCoreOSAge() int64 {
	return int64(time.Since(time.Unix(CoreOSEpoch, 0)) / (86400 * time.Second))
}
