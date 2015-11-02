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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/user"
)

const (
	// This command is the absolute bare minimum to enter the SDK
	// and run repo. It *must* be run in a new mount namespace.
	repoScript = "mkdir -p {{.Chroot}}/mnt/host/source && " +
		"mount --make-rslave / && " +
		"mount --bind {{.RepoRoot}} {{.Chroot}}/mnt/host/source && " +
		"exec chroot {{.Chroot}} " +
		"/usr/bin/sudo -u {{.Username}} sh -c " +
		"'cd /mnt/host/source && repo {{.RepoArgs}}'"
	enterChroot = "src/scripts/sdk_lib/enter_chroot.sh"
)

var repoTemplate = template.Must(template.New("script").Parse(repoScript))

type repoParams struct {
	*user.User
	Chroot   string
	RepoRoot string
	RepoArgs string
}

// Set an environment variable if it isn't already defined.
func setDefault(environ []string, key, value string) []string {
	prefix := key + "="
	for _, env := range environ {
		if strings.HasPrefix(env, prefix) {
			return environ
		}
	}
	return append(environ, prefix+value)
}

// Set a default email address so repo doesn't explode on 'u@h.(none)'
func setDefaultEmail(environ []string) []string {
	username := "nobody"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	domain := system.FullHostname()
	email := fmt.Sprintf("%s@%s", username, domain)
	return setDefault(environ, "EMAIL", email)
}

func repo(name, args string) error {
	chroot := filepath.Join(RepoRoot(), name)
	u, err := user.Current()
	if err != nil {
		return err
	}

	params := repoParams{
		User:     u,
		Chroot:   chroot,
		RepoRoot: RepoRoot(),
		RepoArgs: args,
	}

	var sc bytes.Buffer
	if err := repoTemplate.Execute(&sc, &params); err != nil {
		return err
	}

	sh := exec.Command("sudo", sudoPrompt, "-E",
		"unshare", "--mount",
		"sh", "-e", "-c", sc.String())
	sh.Env = setDefaultEmail(os.Environ())
	sh.Stdin = os.Stdin
	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr

	return sh.Run()
}

func Enter(name string, args ...string) error {
	chroot := filepath.Join(RepoRoot(), name)

	// TODO(marineam): the original cros_sdk uses a white list to
	// selectively pass through environment variables instead of the
	// catch-all -E which is probably a better way to do it.
	enterCmd := exec.Command(
		"sudo", sudoPrompt, "-E",
		"unshare", "--mount", "--",
		filepath.Join(RepoRoot(), enterChroot),
		"--chroot", chroot, "--cache_dir", RepoCache(), "--")
	enterCmd.Args = append(enterCmd.Args, args...)
	enterCmd.Env = setDefaultEmail(os.Environ())
	enterCmd.Stdin = os.Stdin
	enterCmd.Stdout = os.Stdout
	enterCmd.Stderr = os.Stderr

	return enterCmd.Run()
}

func RepoInit(name, manifest string) error {
	if err := repo(name, "init -u "+manifest); err != nil {
		return err
	}

	return repo(name, "sync")
}
