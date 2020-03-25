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
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/coreos/mantle/cosa"
	"github.com/coreos/mantle/system"
)

// Build is a parsed coreos-assembler build
type LocalBuild struct {
	Dir  string
	Arch string
	Meta *cosa.Build
}

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

func IsCosaRoot(root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, "builds")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "validating coreos-assembler root")
	}
	return true, nil
}

func RequireCosaRoot(root string) error {
	isroot, err := IsCosaRoot(root)
	if err != nil {
		return err
	}
	if !isroot {
		return fmt.Errorf("%s does not appear to be a coreos-assembler buildroot", root)
	}
	return nil
}

func GetLatestLocalBuild(root string) (*LocalBuild, error) {
	return GetLocalBuild(root, "latest")
}

func GetLocalBuild(root, buildid string) (*LocalBuild, error) {
	if err := RequireCosaRoot(root); err != nil {
		return nil, err
	}

	arch := system.RpmArch()
	builddir := filepath.Join(root, "builds", buildid, arch)
	metapath := filepath.Join(builddir, "meta.json")
	cosameta, err := cosa.ParseBuild(metapath)
	if err != nil {
		return nil, err
	}

	return &LocalBuild{
		Dir:  builddir,
		Arch: arch,
		Meta: cosameta,
	}, nil
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
