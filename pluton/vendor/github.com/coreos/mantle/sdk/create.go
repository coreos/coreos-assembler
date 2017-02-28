// Copyright 2015 CoreOS, Inc.
// Copyright 2011 The Go Authors.
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
	"path/filepath"
	"text/template"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/user"
	"github.com/coreos/mantle/util"
)

// Must run inside the SDK chroot, easiest to just assemble a script to do it
const (
	safePath   = "PATH=/usr/sbin:/usr/bin:/sbin:/bin"
	sudoPrompt = "--prompt=sudo password for %p: "
	script     = `#!/bin/bash
set -e

# make sure user/group database files exist
touch /etc/{group,gshadow,passwd,shadow}
chmod 0640 /etc/{gshadow,shadow}

# add group if it doesn't exist already
if ! getent group {{printf "%q" .Groupname}} >/dev/null; then
	echo Adding group {{printf "%q" .Groupname}}
	groupadd -o -g {{.Gid}} {{printf "%q" .Groupname}}
fi

# add user if it doesn't exist already
if ! getent passwd {{printf "%q" .Username}} >/dev/null; then
	echo Adding user {{printf "%q" .Username}}
	useradd -o -g {{.Gid}} -u {{.Uid}} -s /bin/bash -m \
		-c {{printf "%q" .Name}} {{printf "%q" .Username}}
fi

for g in kvm portage sudo; do
	# copy system group from /usr to /etc if needed
	if getent -s usrfiles group "$g" >/dev/null && \
	   ! getent -s files group "$g" >/dev/null; then
		getent -s usrfiles group "$g" >> /etc/group
	fi
	gpasswd -a {{printf "%q" .Username}} "$g"
done

echo Setting up sudoers
cat >/etc/sudoers.d/90_env_keep <<EOF
Defaults env_keep += "\
COREOS_BUILD_ID COREOS_OFFICIAL \
EMAIL GIT_AUTHOR_EMAIL GIT_AUTHOR_NAME \
GIT_COMMITTER_EMAIL GIT_COMMITTER_NAME \
GIT_PROXY_COMMAND GIT_SSH RSYNC_PROXY \
GNUPGHOME GPG_AGENT_INFO SSH_AUTH_SOCK \
BOTO_PATH GOOGLE_APPLICATION_CREDENTIALS \
USE FEATURES PORTAGE_USERNAME \
all_proxy ftp_proxy http_proxy https_proxy no_proxy"
EOF
chmod 0440 /etc/sudoers.d/90_env_keep

echo Setting default enviornment variables
cat >/etc/env.d/90portage_username <<EOF
PORTAGE_USERNAME={{printf "%q" .Username}}
EOF
# needlessly noisy since portage isn't set up yet
env-update &>/dev/null

echo Setting up home directory
HOME=/home/{{printf "%q" .Username}}

# Create ~/trunk symlink
ln -sfT /mnt/host/source "$HOME"/trunk

rm -f "$HOME"/.bash{_logout,_profile,rc}
cat >"$HOME"/.bash_logout <<EOF
# .bash_logout

# This file is sourced when a login shell terminates.
EOF

cat >"$HOME"/.bash_profile <<EOF
# .bash_profile

# This file is sourced by bash for login shells.  The following line
# runs your .bashrc and is recommended by the bash info pages.
[[ -f ~/.bashrc ]] && . ~/.bashrc

# Automatically change to scripts directory.
cd ${CHROOT_CWD:-~/trunk/src/scripts}
EOF

cat >"$HOME"/.bashrc <<EOF
# .bashrc

# This file is sourced by all *interactive* bash shells on startup,
# including some apparently interactive shells such as scp and rcp
# that can't tolerate any output.  So make sure this doesn't display
# anything or bad things will happen !

# Test for an interactive shell.  There is no need to set anything
# past this point for scp and rcp, and it's important to refrain from
# outputting anything in those cases.
if [[ $- != *i* ]] ; then
	# Shell is non-interactive.  Be done now!
	return
fi

# Enable bash completion for build scripts.
source ~/trunk/src/scripts/bash_completion

# Put your fun stuff here.
EOF

chown -R {{.Uid}}:{{.Gid}} "$HOME"

# Checked in src/scripts/common.sh
touch /etc/debian_chroot
`
)

var scriptTemplate = template.Must(template.New("script").Parse(script))

func Setup(name string) error {
	chroot := filepath.Join(RepoRoot(), name)
	u, err := user.Current()
	if err != nil {
		return err
	}

	var sc bytes.Buffer
	if err := scriptTemplate.Execute(&sc, u); err != nil {
		return err
	}

	plog.Info("Configuring SDK chroot")
	sh := exec.Command(
		"sudo", sudoPrompt,
		"chroot", chroot,
		"/usr/bin/env", "-i",
		"/bin/bash", "--login")
	sh.Stdin = &sc
	sh.Stderr = os.Stderr
	if plog.LevelAt(capnslog.INFO) {
		out, err := sh.StdoutPipe()
		if err != nil {
			return err
		}
		go util.LogFrom(capnslog.INFO, out)
	}
	if plog.LevelAt(capnslog.DEBUG) {
		sh.Args = append(sh.Args, "-x")
	}
	return sh.Run()
}

func extract(tar, dir string) error {
	in, err := os.Open(tar)
	if err != nil {
		return err
	}
	defer in.Close()

	unzipper, err := exec.LookPath("lbzcat")
	if err != nil {
		unzipper = "bzcat"
	}

	unzip := exec.Command(unzipper)
	unzip.Stdin = in
	unzip.Stderr = os.Stderr
	unzipped, err := unzip.StdoutPipe()
	if err != nil {
		return err
	}

	untar := exec.Command("sudo", sudoPrompt,
		"tar", "--numeric-owner", "-x")
	untar.Dir = dir
	untar.Stdin = unzipped
	untar.Stderr = os.Stderr

	if err := unzip.Start(); err != nil {
		return err
	}

	if err := untar.Start(); err != nil {
		unzip.Kill()
		return err
	}

	if err := untar.Wait(); err != nil {
		unzip.Kill()
		return err
	}

	if err := unzip.Wait(); err != nil {
		return err
	}

	return nil
}

func Unpack(version, name string) error {
	chroot := filepath.Join(RepoRoot(), name)
	if _, err := os.Stat(chroot); !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("Path already exists: %s", chroot)
		}
		return err
	}

	plog.Noticef("Unpacking SDK into %s", chroot)
	if err := os.MkdirAll(chroot, 0777); err != nil {
		return err
	}

	tar := filepath.Join(RepoCache(), "sdks", TarballName(version))
	plog.Infof("Using %s", tar)
	if err := extract(tar, chroot); err != nil {
		plog.Errorf("Extracting %s to %s failed: %v", tar, chroot, err)
		return err
	}
	plog.Notice("Unpacked")

	return nil
}

func Delete(name string) error {
	chroot := filepath.Join(RepoRoot(), name)
	if _, err := os.Stat(chroot); err != nil {
		if os.IsNotExist(err) {
			plog.Infof("Path does not exist: %s", chroot)
			return nil
		}
		return err
	}

	plog.Noticef("Removing SDK at %s", chroot)
	rm := exec.Command("sudo", sudoPrompt, "rm", "-rf", chroot)
	rm.Stderr = os.Stderr
	if err := rm.Run(); err != nil {
		return err
	}
	plog.Notice("Removed")

	return nil
}
