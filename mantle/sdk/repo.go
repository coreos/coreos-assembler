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

	"github.com/coreos/mantle/system"
)

const ()

func isDir(dir string) bool {
	stat, err := os.Stat(dir)
	return err == nil && stat.IsDir()
}

func envDir(env string) string {
	dir := os.Getenv(env)
	if dir == "" {
		return ""
	}
	if !filepath.IsAbs(dir) {
		log.Fatalf("%s is not an absolute path: %q", env, dir)
	}
	return dir
}

func RepoRoot() string {
	wd, _ := os.Getwd()
	return wd
}

func DefaultBoard() string {
	defaultBoard := system.PortageArch() + "-usr"
	return string(defaultBoard)
}

// TODO replace with coreos-assembler concepts
func BoardRoot(board string) string {
	return ""
}

// TODO replace with coreos-assembler concepts
func BuildRoot() string {
	return ""
}
