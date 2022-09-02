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

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	cosa "github.com/coreos/coreos-assembler/pkg/builds"
)

const (
	// fastBuildCosaDir is used by build-fast
	fastBuildCosaDir = ".cosa"
)

// Build is a parsed coreos-assembler build
type LocalBuild struct {
	Dir  string
	Arch string
	Meta *cosa.Build
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

// GetLocalFastBuildQemu finds content written by `cosa build-fast`
func GetLocalFastBuildQemu() (string, error) {
	if _, err := os.Stat(fastBuildCosaDir); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	ents, err := os.ReadDir(fastBuildCosaDir)
	if err != nil {
		return "", err
	}
	for _, ent := range ents {
		if strings.HasSuffix(ent.Name(), ".qcow2") {
			return filepath.Join(".cosa", ent.Name()), nil
		}
	}
	return "", nil
}

func GetLatestLocalBuild(root, arch string) (*LocalBuild, error) {
	return GetLocalBuild(root, "latest", arch)
}

func GetLocalBuild(root, buildid, arch string) (*LocalBuild, error) {
	if err := RequireCosaRoot(root); err != nil {
		return nil, err
	}

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
