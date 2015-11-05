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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/user"
)

const enterChroot = "src/scripts/sdk_lib/enter_chroot.sh"

var simpleChroot exec.Entrypoint

func init() {
	simpleChroot = exec.NewEntrypoint("simpleChroot", simpleChrootHelper)
}

// bind mount the repo source tree into the chroot and run a command
func simpleChrootHelper(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("got %d args, need at least 3", len(args))
	}
	hostRepoRoot := args[0]
	chroot := args[1]
	chrootCmd := args[2:]
	username := os.Getenv("SUDO_USER")
	if username == "" {
		return fmt.Errorf("SUDO_USER environment variable is not set.")
	}

	newRepoRoot := filepath.Join(chroot, chrootRepoRoot)
	if err := os.MkdirAll(newRepoRoot, 0755); err != nil {
		return err
	}

	// namespaces are per-thread attributes
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
		return fmt.Errorf("Unsharing mount namespace failed: %v", err)
	}

	if err := syscall.Mount(
		"none", "/", "none", syscall.MS_REC|syscall.MS_SLAVE, ""); err != nil {
		return fmt.Errorf("Unsharing mount points failed: %v", err)
	}

	if err := syscall.Mount(
		hostRepoRoot, newRepoRoot, "none", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("Mounting %q failed: %v", newRepoRoot, err)
	}

	if err := syscall.Chroot(chroot); err != nil {
		return fmt.Errorf("Chrooting to %q failed: %v", chroot, err)
	}

	if err := os.Chdir(chrootRepoRoot); err != nil {
		return err
	}

	sudo := "/usr/bin/sudo"
	sudoArgs := append([]string{sudo, "-u", username, "--"}, chrootCmd...)
	return syscall.Exec(sudo, sudoArgs, os.Environ())
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

func SimpleEnter(name string, args ...string) error {
	reroot := RepoRoot()
	chroot := filepath.Join(reroot, name)
	args = append([]string{reroot, chroot}, args...)

	sudo := simpleChroot.Sudo(args...)
	sudo.Env = setDefaultEmail(os.Environ())
	sudo.Stdin = os.Stdin
	sudo.Stdout = os.Stdout
	sudo.Stderr = os.Stderr

	return sudo.Run()
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
	if err := SimpleEnter(name, "repo", "init", "-u", manifest); err != nil {
		return err
	}

	return SimpleEnter(name, "repo", "sync")
}
