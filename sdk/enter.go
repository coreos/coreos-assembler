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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/user"
)

const enterChrootSh = "src/scripts/sdk_lib/enter_chroot.sh"

var (
	enterChrootCmd exec.Entrypoint

	botoTemplate = template.Must(template.New("boto").Parse(`
{{if eq .Type "authorized_user"}}
[Credentials]
gs_oauth2_refresh_token = {{.RefreshToken}}
[OAuth2]
client_id = {{.ClientID}}
client_secret = {{.ClientSecret}}
{{else}}{{if eq .Type "service_account"}}
[Credentials]
gs_service_key_file = {{.Path}}
{{end}}{{end}}
`))
)

func init() {
	enterChrootCmd = exec.NewEntrypoint("enterChroot", enterChrootHelper)
}

// Information on the chroot. Except for Cmd and CmdDir paths
// are relative to the host system.
type enter struct {
	RepoRoot   string
	Chroot     string
	Cmd        []string
	CmdDir     string
	User       *user.User
	UserRunDir string
}

type googleCreds struct {
	// Path to JSON file (for template above)
	Path string

	// Common fields
	Type     string
	ClientID string `json:"client_id"`

	// User Credential fields
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`

	// Service Account fields
	ClientEmail  string `json:"client_email"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
}

// MountAPI mounts standard Linux API filesystems.
// When possible the filesystems are mounted read-only.
func (e *enter) MountAPI() error {
	var apis = []struct {
		Path string
		Type string
		Opts string
	}{
		{"/proc", "proc", "ro,nosuid,nodev,noexec"},
		{"/sys", "sysfs", "ro,nosuid,nodev,noexec"},
		{"/run", "tmpfs", "nosuid,nodev,mode=755"},
	}

	for _, fs := range apis {
		target := filepath.Join(e.Chroot, fs.Path)
		if err := system.Mount("", target, fs.Type, fs.Opts); err != nil {
			return err
		}
	}

	// Since loop devices are dynamic we need the host's managed /dev
	if err := system.ReadOnlyBind("/dev", filepath.Join(e.Chroot, "dev")); err != nil {
		return err
	}
	// /dev/pts must be read-write because emerge chowns tty devices.
	if err := system.Bind("/dev/pts", filepath.Join(e.Chroot, "dev/pts")); err != nil {
		return err
	}

	// Unfortunately using the host's /dev complicates /dev/shm which may
	// be a directory or a symlink into /run depending on the distro. :(
	// XXX: catalyst does not work on systems with a /dev/shm symlink!
	if system.IsSymlink("/dev/shm") {
		shmPath, err := filepath.EvalSymlinks("/dev/shm")
		if err != nil {
			return err
		}
		// Only accept known values to avoid surprises.
		if shmPath != "/run/shm" {
			return fmt.Errorf("Unexpected shm path: %s", shmPath)
		}
		newPath := filepath.Join(e.Chroot, shmPath)
		if err := os.Mkdir(newPath, 01777); err != nil {
			return err
		}
		if err := os.Chmod(newPath, 01777); err != nil {
			return err
		}
	} else {
		shmPath := filepath.Join(e.Chroot, "dev/shm")
		if err := system.Mount("", shmPath, "tmpfs", "nosuid,nodev"); err != nil {
			return err
		}
	}

	return nil
}

// MountAgent bind mounts a SSH or GnuPG agent socket into the chroot
func (e *enter) MountAgent(env string) error {
	origPath := os.Getenv(env)
	if origPath == "" {
		return nil
	}

	origDir, origFile := filepath.Split(origPath)
	if _, err := os.Stat(origDir); err != nil {
		// Just skip if the agent has gone missing.
		return nil
	}

	newDir, err := ioutil.TempDir(e.UserRunDir, "agent-")
	if err != nil {
		return err
	}

	if err := system.Bind(origDir, newDir); err != nil {
		return err
	}

	newPath := filepath.Join(newDir, origFile)
	chrootPath := strings.TrimPrefix(newPath, e.Chroot)
	return os.Setenv(env, chrootPath)
}

// MountGnupg bind mounts $GNUPGHOME or ~/.gnupg and the agent socket
// if available. The agent is ignored if the home dir isn't available.
func (e *enter) MountGnupg() error {
	origHome := os.Getenv("GNUPGHOME")
	if origHome == "" {
		origHome = filepath.Join(e.User.HomeDir, ".gnupg")
	}

	if _, err := os.Stat(origHome); err != nil {
		// Skip but do not pass along $GNUPGHOME
		return os.Unsetenv("GNUPGHOME")
	}

	newHome, err := ioutil.TempDir(e.UserRunDir, "gnupg-")
	if err != nil {
		return err
	}

	if err := system.Bind(origHome, newHome); err != nil {
		return err
	}

	chrootHome := strings.TrimPrefix(newHome, e.Chroot)
	if err := os.Setenv("GNUPGHOME", chrootHome); err != nil {
		return err
	}

	return e.MountAgent("GPG_AGENT_INFO")
}

// CopyGoogleCreds copies a Google credentials JSON file if one exists.
// Unfortunately gsutil only partially supports these JSON files and does not
// respect GOOGLE_APPLICATION_CREDENTIALS at all so a boto file is created.
// TODO(marineam): integrate with mantle/auth package to migrate towards
// consistent handling of credentials across all of mantle and the SDK.
func (e *enter) CopyGoogleCreds() error {
	const (
		name = "application_default_credentials.json"
		env  = "GOOGLE_APPLICATION_CREDENTIALS"
	)

	path := os.Getenv(env)
	if path == "" {
		path = filepath.Join(e.User.HomeDir, ".config", "gcloud", name)
	}

	if _, err := os.Stat(path); err != nil {
		// Skip but do not pass along the invalid env var
		os.Unsetenv("BOTO_PATH")
		return os.Unsetenv(env)
	}

	newDir, err := ioutil.TempDir(e.UserRunDir, "google-")
	if err != nil {
		return err
	}
	if err := os.Chown(newDir, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}
	newPath := filepath.Join(newDir, name)
	chrootPath := strings.TrimPrefix(newPath, e.Chroot)

	credsRaw, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	var creds googleCreds
	if err := json.Unmarshal(credsRaw, &creds); err != nil {
		return err
	}
	creds.Path = chrootPath

	botoPath := filepath.Join(newDir, "boto")
	boto, err := os.OpenFile(botoPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer boto.Close()

	if err := botoTemplate.Execute(boto, &creds); err != nil {
		return err
	}

	if err := boto.Chown(e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	// Include the default boto path as well for user customization.
	chrootBoto := fmt.Sprintf("%s:/home/%s/.boto",
		strings.TrimPrefix(botoPath, e.Chroot), e.User.Username)
	if err := os.Setenv("BOTO_PATH", chrootBoto); err != nil {
		return err
	}

	if err := system.CopyRegularFile(path, newPath); err != nil {
		return err
	}

	if err := os.Chown(newPath, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	return os.Setenv(env, chrootPath)
}

// bind mount the repo source tree into the chroot and run a command
func enterChrootHelper(args []string) (err error) {
	if len(args) < 3 {
		return fmt.Errorf("got %d args, need at least 3", len(args))
	}

	e := enter{
		RepoRoot: args[0],
		Chroot:   args[1],
		CmdDir:   args[2],
		Cmd:      args[3:],
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

	if err := system.RecursiveSlave("/"); err != nil {
		return err
	}

	if err := system.Bind(e.RepoRoot, newRepoRoot); err != nil {
		return err
	}

	if err := e.MountAPI(); err != nil {
		return err
	}

	if err = os.MkdirAll(e.UserRunDir, 0755); err != nil {
		return err
	}

	if err = os.Chown(e.UserRunDir, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	if err := e.MountAgent("SSH_AUTH_SOCK"); err != nil {
		return err
	}

	if err := e.MountGnupg(); err != nil {
		return err
	}

	if err := e.CopyGoogleCreds(); err != nil {
		return err
	}

	if err := syscall.Chroot(e.Chroot); err != nil {
		return fmt.Errorf("Chrooting to %q failed: %v", e.Chroot, err)
	}

	if err := os.Chdir(e.CmdDir); err != nil {
		return err
	}

	sudo := "/usr/bin/sudo"
	sudoArgs := append([]string{sudo, "-u", username}, e.Cmd...)
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

// Enter the chroot and run a command in the given dir. The args get passed
// directly to sudo so things like -i for a login shell are allowed.
func enterChroot(name, dir string, args ...string) error {
	reroot := RepoRoot()
	chroot := filepath.Join(reroot, name)
	args = append([]string{reroot, chroot, dir}, args...)

	sudo := enterChrootCmd.Sudo(args...)
	sudo.Env = setDefaultEmail(os.Environ())
	sudo.Stdin = os.Stdin
	sudo.Stdout = os.Stdout
	sudo.Stderr = os.Stderr

	if err := copyUserConfig(chroot); err != nil {
		return err
	}

	return sudo.Run()
}

// Enter the chroot with a login shell, optionally invoking a command.
// The command may be prefixed by environment variable assignments.
func Enter(name string, args ...string) error {
	// pass -i to sudo to invoke a login shell
	cmd := []string{"-i", "--"}
	if len(args) > 0 {
		cmd = append(cmd, "env", "--")
		cmd = append(cmd, args...)
	}
	// the directory doesn't matter here, sudo -i will chdir to $HOME
	return enterChroot(name, "/", cmd...)
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
