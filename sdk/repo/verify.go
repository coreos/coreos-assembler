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

package repo

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/system/exec"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "sdk/repo")

	Unimplemented = errors.New("repo: unimplemented feature in manifest")
	MissingField  = errors.New("repo: missing required field in manifest")
	VerifyError   = errors.New("repo: failed verification")
)

type repo struct {
	Manifest
	root string
	name string
}

func (r *repo) load(name string) (err error) {
	r.root = sdk.RepoRoot()
	path := filepath.Join(r.root, ".repo")
	if len(name) != 0 {
		path = filepath.Join(path, "manifests", name)
		r.name = name
	} else {
		path = filepath.Join(path, "manifest.xml")
		r.name = "manifest" // just need something for errs
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if err = xml.NewDecoder(file).Decode(&r.Manifest); err != nil {
		return err
	}

	// Check for currently unsupported features.
	assertEmpty := func(l int, f string) {
		if l == 0 {
			return
		}
		plog.Errorf("Unsupported feature %s in %s", f, r.name)
		err = Unimplemented
	}
	assertEmpty(len(r.Includes), "include")
	assertEmpty(len(r.ExtendProjects), "extend-project")
	assertEmpty(len(r.RemoveProjects), "remove-project")
	for _, project := range r.Projects {
		if len(project.SubProjects) != 0 {
			plog.Errorf("Unsupported sub-project in %s", r.name)
			err = Unimplemented
			break
		}
	}

	return err
}

func (r *repo) fillDefaults() (err error) {
	for _, project := range r.Projects {
		if project.Name == "" {
			plog.Errorf("Project missing name in %s", r.name)
			err = MissingField
		}

		if project.Path == "" {
			project.Path = project.Name
		}

		if project.Remote == "" {
			project.Remote = r.Default.Remote
		}
		if project.Remote == "" {
			plog.Errorf("Project %s missing remote in %s",
				project.Name, r.name)
			err = MissingField
		}

		if project.Revision == "" {
			project.Revision = r.Default.Revision
		}
		if project.Revision == "" {
			plog.Errorf("Project %s missing revision in %s",
				project.Name, r.name)
			err = MissingField
		}

		if project.DestBranch == "" {
			project.DestBranch = r.Default.DestBranch
		}
		if project.SyncBranch == "" {
			project.SyncBranch = r.Default.SyncBranch
		}
		if project.SyncSubProjects == "" {
			project.SyncSubProjects = r.Default.SyncSubProjects
		}
	}
	return err
}

func isSHA1(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha1.Size
}

func (r *repo) projectHEAD(p Project) (string, error) {
	git := exec.Command("git", "rev-list", "--max-count=1", "HEAD")
	git.Dir = filepath.Join(r.root, p.Path)
	git.Stderr = os.Stderr
	out, err := git.Output()
	if err != nil {
		return "", err
	}

	rev := string(bytes.TrimSpace(out))
	if !isSHA1(rev) {
		return "", fmt.Errorf("%s: bad revision %s", p.Path, rev)
	}

	return rev, nil
}

func (r *repo) projectIsClean(p Project) error {
	git := exec.Command("git", "diff", "--quiet")
	git.Dir = filepath.Join(r.root, p.Path)
	git.Stdout = os.Stdout
	git.Stderr = os.Stderr
	return git.Run()
}

// VerifySync takes a manifest name and asserts the current repo checkout
// matches it exactly. Currently only supports manifests flattened by the
// "repo manifest -r" command. A blank name means to use the default.
// TODO: check symbolic refs too? e.g. HEAD == refs/remotes/origin/master
func VerifySync(name string) error {
	var manifest repo
	if err := manifest.load(name); err != nil {
		return err
	}

	if err := manifest.fillDefaults(); err != nil {
		return err
	}

	var result error
	for _, project := range manifest.Projects {
		if !isSHA1(project.Revision) {
			plog.Errorf("Cannot verify %s revision %s in %s",
				project.Name, project.Revision, manifest.name)
			return Unimplemented
		}

		rev, err := manifest.projectHEAD(project)
		if err != nil {
			return err
		}

		if rev != project.Revision {
			plog.Errorf("Project dir %s at %s, expected %s",
				project.Path, rev, project.Revision)
			result = VerifyError
		}

		if err := manifest.projectIsClean(project); err != nil {
			plog.Errorf("Project dir %s is not clean, git diff %v",
				project.Path, err)
			result = VerifyError
		}
	}
	return result
}
