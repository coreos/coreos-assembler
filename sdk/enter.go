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
gs_service_key_file = {{.JsonPath}}
{{end}}{{end}}
[GSUtil]
state_dir = {{.StateDir}}
`))
)

const (
	defaultResolv = "nameserver 8.8.8.8\nnameserver 8.8.4.4\n"
)

func init() {
	enterChrootCmd = exec.NewEntrypoint("enterChroot", enterChrootHelper)
}

// Information on the chroot. Except for Cmd and CmdDir paths
// are relative to the host system.
type enter struct {
	RepoRoot     string     `json:",omitempty"`
	Chroot       string     `json:",omitempty"`
	Cmd          []string   `json:",omitempty"`
	CmdDir       string     `json:",omitempty"`
	BindGpgAgent bool       `json:",omitempty"`
	UseHostDNS   bool       `json:",omitempty"`
	User         *user.User `json:",omitempty"`
	UserRunDir   string     `json:",omitempty"`
}

type googleCreds struct {
	// Path to JSON file (for template above)
	JsonPath string

	// Path gsutil will store cached credentials and other state.
	// Must contain a pre-created 'tracker-files' directory because
	// gsutil sometimes creates it with an inappropriate umask.
	StateDir string

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

	// Make sure the new root directory itself is a mount point.
	// `unshare` assumes that `mount --make-rprivate /` works.
	if err := system.RecursiveBind(e.Chroot, e.Chroot); err != nil {
		return err
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
func (e *enter) MountSSHAgent() error {
	origPath := os.Getenv("SSH_AUTH_SOCK")
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
	return os.Setenv("SSH_AUTH_SOCK", chrootPath)
}

// MountGnupg bind mounts $GNUPGHOME or ~/.gnupg and the agent socket
// if available. The agent is ignored if the home dir isn't available.
func (e *enter) MountGnupgHome() error {
	origHome := os.Getenv("GNUPGHOME")
	if origHome == "" {
		origHome = filepath.Join(e.User.HomeDir, ".gnupg")
	}

	if _, err := os.Stat(origHome); err != nil {
		// Skip but do not pass along $GNUPGHOME
		return os.Unsetenv("GNUPGHOME")
	}

	// gpg gets confused when GNUPGHOME isn't ~/.gnupg, so mount it there.
	// Additionally, set the GNUPGHOME variable so commands run with sudo
	// can also use it.
	newHomeInChroot := filepath.Join("/home", e.User.Username, ".gnupg")
	newHome := filepath.Join(e.Chroot, newHomeInChroot)
	if err := os.Mkdir(newHome, 0700); err != nil && !os.IsExist(err) {
		return err
	}

	if err := system.Bind(origHome, newHome); err != nil {
		return err
	}

	return os.Setenv("GNUPGHOME", newHomeInChroot)
}

func (e *enter) MountGnupgAgent() error {
	// Newer GPG releases make it harder to find out what dir has the sockets
	// so use /run/user/$uid/gnupg which is the default
	origAgentDir := filepath.Join("/run", "user", e.User.Uid, "gnupg")
	if _, err := os.Stat(origAgentDir); err != nil {
		// Skip
		return nil
	}

	// gpg acts weird if this is elsewhere, so use /run/user/$uid/gnupg
	newAgentDir := filepath.Join(e.Chroot, origAgentDir)
	if err := os.Mkdir(newAgentDir, 0700); err != nil && !os.IsExist(err) {
		return err
	}

	return system.Bind(origAgentDir, newAgentDir)
}

// CopyGoogleCreds copies a Google credentials JSON file if one exists.
// Unfortunately gsutil only partially supports these JSON files and does not
// respect GOOGLE_APPLICATION_CREDENTIALS at all so a boto file is created.
// TODO(marineam): integrate with mantle/auth package to migrate towards
// consistent handling of credentials across all of mantle and the SDK.
func (e *enter) CopyGoogleCreds() error {
	const (
		botoName    = "boto"
		jsonName    = "application_default_credentials.json"
		trackerName = "tracker-files"
		botoEnvName = "BOTO_PATH"
		jsonEnvName = "GOOGLE_APPLICATION_CREDENTIALS"
	)

	jsonSrc := os.Getenv(jsonEnvName)
	if jsonSrc == "" {
		jsonSrc = filepath.Join(e.User.HomeDir, ".config", "gcloud", jsonName)
	}

	if _, err := os.Stat(jsonSrc); err != nil {
		// Skip but do not pass along the invalid env var
		os.Unsetenv(botoEnvName)
		return os.Unsetenv(jsonEnvName)
	}

	stateDir, err := ioutil.TempDir(e.UserRunDir, "google-")
	if err != nil {
		return err
	}
	if err := os.Chown(stateDir, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	var (
		botoPath       = filepath.Join(stateDir, botoName)
		jsonPath       = filepath.Join(stateDir, jsonName)
		trackerDir     = filepath.Join(stateDir, trackerName)
		chrootBotoPath = strings.TrimPrefix(botoPath, e.Chroot)
		chrootJsonPath = strings.TrimPrefix(jsonPath, e.Chroot)
		chrootStateDir = strings.TrimPrefix(stateDir, e.Chroot)
	)

	if err := os.Mkdir(trackerDir, 0700); err != nil {
		return err
	}
	if err := os.Chown(trackerDir, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	credsRaw, err := ioutil.ReadFile(jsonSrc)
	if err != nil {
		return err
	}
	var creds googleCreds
	if err := json.Unmarshal(credsRaw, &creds); err != nil {
		return fmt.Errorf("Unmarshal GoogleCreds failed: %s", err)
	}
	creds.JsonPath = chrootJsonPath
	creds.StateDir = chrootStateDir

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
	botoEnv := fmt.Sprintf("%s:/home/%s/.boto", chrootBotoPath, e.User.Username)
	if err := os.Setenv(botoEnvName, botoEnv); err != nil {
		return err
	}

	if err := system.CopyRegularFile(jsonSrc, jsonPath); err != nil {
		return err
	}

	if err := os.Chown(jsonPath, e.User.UidNo, e.User.GidNo); err != nil {
		return err
	}

	return os.Setenv(jsonEnvName, jsonPath)
}

func (e enter) SetupDNS() error {
	resolv := "/etc/resolv.conf"
	chrootResolv := filepath.Join(e.Chroot, resolv)
	if !e.UseHostDNS {
		return ioutil.WriteFile(chrootResolv, []byte(defaultResolv), 0644)
	}

	if _, err := os.Stat(resolv); err == nil {
		// Only copy if resolv.conf exists, if missing resolver uses localhost
		return system.InstallRegularFile(resolv, chrootResolv)
	}
	return nil
}

// bind mount the repo source tree into the chroot and run a command
// Called via the multicall interface. Should only have 1 arg which is an
// enter struct encoded in json.
func enterChrootHelper(args []string) (err error) {
	if len(args) != 1 {
		return fmt.Errorf("got %d args, need exactly 1", len(args))
	}

	var e enter
	if err := json.Unmarshal([]byte(args[0]), &e); err != nil {
		return err
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

	if err := e.SetupDNS(); err != nil {
		return err
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

	if err := system.RecursiveBind(e.RepoRoot, newRepoRoot); err != nil {
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

	if err := e.MountSSHAgent(); err != nil {
		return err
	}

	if err := e.MountGnupgHome(); err != nil {
		return err
	}

	if e.BindGpgAgent {
		if err := e.MountGnupgAgent(); err != nil {
			return err
		}
	}

	if err := e.CopyGoogleCreds(); err != nil {
		return err
	}

	if err := syscall.Chroot(e.Chroot); err != nil {
		return fmt.Errorf("Chrooting to %q failed: %v", e.Chroot, err)
	}

	if e.CmdDir != "" {
		if err := os.Chdir(e.CmdDir); err != nil {
			return err
		}
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

// Enter the chroot and run a command in the given dir. The args specified in cmd
//get passed directly to sudo so things like -i for a login shell are allowed.
func enterChroot(e enter) error {
	if e.RepoRoot == "" {
		e.RepoRoot = RepoRoot()
	}
	if e.Chroot == "" {
		e.Chroot = "chroot"
	}
	e.Chroot = filepath.Join(e.RepoRoot, e.Chroot)

	enterJson, err := json.Marshal(e)
	if err != nil {
		return err
	}
	sudo := enterChrootCmd.Sudo(string(enterJson))
	sudo.Env = setDefaultEmail(os.Environ())
	sudo.Stdin = os.Stdin
	sudo.Stdout = os.Stdout
	sudo.Stderr = os.Stderr

	if err := copyUserConfig(e.Chroot); err != nil {
		return err
	}

	// will call enterChrootHelper via the multicall interface
	return sudo.Run()
}

// Enter the chroot with a login shell, optionally invoking a command.
// The command may be prefixed by environment variable assignments.
func Enter(name string, bindGpgAgent, useHostDNS bool, args ...string) error {
	// pass -i to sudo to invoke a login shell
	cmd := []string{"-i", "--"}
	if len(args) > 0 {
		cmd = append(cmd, "env", "--")
		cmd = append(cmd, args...)
	}
	// the CmdDir doesn't matter here, sudo -i will chdir to $HOME
	e := enter{
		Chroot:       name,
		Cmd:          cmd,
		BindGpgAgent: bindGpgAgent,
		UseHostDNS:   useHostDNS,
	}
	return enterChroot(e)
}
