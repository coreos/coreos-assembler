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
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/user"
)

const enterChrootSh = "src/scripts/sdk_lib/enter_chroot.sh"

var enterChroot exec.Entrypoint

func init() {
	enterChroot = exec.NewEntrypoint("enterChroot", enterChrootHelper)
}

// Information on the chroot, paths are relative to the host system.
type enter struct {
	RepoRoot   string
	Chroot     string
	Cmd        []string
	User       *user.User
	UserRunDir string
}

// bind mount the repo source tree into the chroot and run a command
func enterChrootHelper(args []string) (err error) {
	if len(args) < 3 {
		return fmt.Errorf("got %d args, need at least 3", len(args))
	}

	e := enter{
		RepoRoot: args[0],
		Chroot:   args[1],
		Cmd:      args[2:],
	}

	username := os.Getenv("SUDO_USER")
	if username == "" {
		return fmt.Errorf("SUDO_USER environment variable is not set.")
	}
	if e.User, err = user.Lookup(username); err != nil {
		return err
	}
	e.UserRunDir = filepath.Join(e.Chroot, "run", "user", e.User.Uid)

	newRepoRoot := filepath.Join(e.Chroot, chrootRepoRoot)
	if err := os.MkdirAll(newRepoRoot, 0755); err != nil {
		return err
	}

	// Only copy if resolv.conf exists, if missing resolver uses localhost
	resolv := "/etc/resolv.conf"
	if _, err := os.Stat(resolv); err == nil {
		chrootResolv := filepath.Join(e.Chroot, resolv)
		if err := system.InstallRegularFile(resolv, chrootResolv); err != nil {
			return err
		}
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
		e.RepoRoot, newRepoRoot, "none", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("Mounting %q failed: %v", newRepoRoot, err)
	}

	// mount the /run directory and setup the permissions
	runMountDir := filepath.Join(e.Chroot, "run")
	if err := syscall.Mount(
		"tmpfs", runMountDir, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "mode=755"); err != nil {
		return fmt.Errorf("Mounting %q failed: %v", runMountDir, err)
	}

	if err = os.MkdirAll(e.UserRunDir, 0755); err != nil {
		return err
	}

	if err = os.Chown(e.UserRunDir, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	// mount the directory containing the ssh-agent socket in order
	// to support a private repos during repo sync
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock != "" {
		sshSourceDir, sshSocketFile := filepath.Split(sshAuthSock)
		if _, err := os.Stat(sshSourceDir); err == nil {
			sshTargetdir, err := ioutil.TempDir(e.UserRunDir, "ssh-")
			if err != nil {
				return err
			}

			if err := syscall.Mount(
				sshSourceDir, sshTargetdir, "none", syscall.MS_BIND, ""); err != nil {
				return fmt.Errorf("Mounting %q failed: %v", sshSourceDir, err)
			}

			os.Setenv("SSH_AUTH_SOCK",
				filepath.Join(strings.TrimPrefix(sshTargetdir, e.Chroot), sshSocketFile))
		}
	}

	if err := syscall.Chroot(e.Chroot); err != nil {
		return fmt.Errorf("Chrooting to %q failed: %v", e.Chroot, err)
	}

	if err := os.Chdir(chrootRepoRoot); err != nil {
		return err
	}

	sudo := "/usr/bin/sudo"
	sudoArgs := append([]string{sudo, "-u", username, "--"}, e.Cmd...)
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

// copies a user's config file from user's home directory to the equivalent
// location in the chroot
func copyUserConfigFile(source, chroot string) error {
	userInfo, err := user.Current()
	if err != nil {
		return err
	}

	sourcepath := filepath.Join(userInfo.HomeDir, source)
	if _, err := os.Stat(sourcepath); err != nil {
		return nil
	}

	chrootHome := filepath.Join(chroot, "home", userInfo.Username)
	sourceDir := filepath.Dir(source)
	if sourceDir != "." {
		if err := os.MkdirAll(
			filepath.Join(chrootHome, sourceDir), 0700); err != nil {
			return err
		}
	}

	tartgetpath := filepath.Join(chrootHome, source)
	if err := system.CopyRegularFile(sourcepath, tartgetpath); err != nil {
		return err
	}

	return nil
}

func copyUserConfig(chroot string) error {
	if err := copyUserConfigFile(".ssh/config", chroot); err != nil {
		return err
	}

	if err := copyUserConfigFile(".ssh/known_hosts", chroot); err != nil {
		return err
	}

	if err := copyUserConfigFile(".gitconfig", chroot); err != nil {
		return err
	}

	return nil
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

func Enter(name string, args ...string) error {
	reroot := RepoRoot()
	chroot := filepath.Join(reroot, name)
	args = append([]string{reroot, chroot}, args...)

	sudo := enterChroot.Sudo(args...)
	sudo.Env = setDefaultEmail(os.Environ())
	sudo.Stdin = os.Stdin
	sudo.Stdout = os.Stdout
	sudo.Stderr = os.Stderr

	if err := copyUserConfig(chroot); err != nil {
		return err
	}

	return sudo.Run()
}

func OldEnter(name string, args ...string) error {
	chroot := filepath.Join(RepoRoot(), name)

	// TODO(marineam): the original cros_sdk uses a white list to
	// selectively pass through environment variables instead of the
	// catch-all -E which is probably a better way to do it.
	enterCmd := exec.Command(
		"sudo", sudoPrompt, "-E",
		"unshare", "--mount", "--",
		filepath.Join(RepoRoot(), enterChrootSh),
		"--chroot", chroot, "--cache_dir", RepoCache(), "--")
	enterCmd.Args = append(enterCmd.Args, args...)
	enterCmd.Env = setDefaultEmail(os.Environ())
	enterCmd.Stdin = os.Stdin
	enterCmd.Stdout = os.Stdout
	enterCmd.Stderr = os.Stderr

	return enterCmd.Run()
}

func RepoInit(name, manifest, manifestName, branch string) error {
	if err := Enter(
		name, "repo", "init", "-u", manifest,
		"-b", branch, "-m", manifestName); err != nil {
		return err
	}

	return Enter(name, "repo", "sync")
}
