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
	"log"
	"os"
	"path/filepath"
)

func isDir(dir string) bool {
	stat, err := os.Stat(dir)
	return err == nil && stat.IsDir()
}

func RepoRoot() string {
	if dir := os.Getenv("REPO_ROOT"); dir != "" {
		dir, err := filepath.Abs(dir)
		if err != nil {
			log.Fatalf("Invalid REPO_ROOT: %v", err)
		}
		return dir
	}

	if isDir("/mnt/host/source") {
		return "/mnt/host/source"
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Invalid working directory: %v", err)
	}

	for dir := wd; ; dir = filepath.Dir(dir) {
		if isDir(filepath.Join(dir, ".repo")) {
			return dir
		} else if filepath.IsAbs(dir) {
			break
		}
	}

	return wd
}

func RepoCache() string {
	return filepath.Join(RepoRoot(), ".cache")
}
