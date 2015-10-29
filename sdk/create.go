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
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"text/template"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/util"
)

/*
#include <grp.h>
#include <stdlib.h>
#include <sys/types.h>
#include <unistd.h>

// Go cannot figure out that gid_t == __gid_t so let C do it.
static int mygetgrgid_r(gid_t gid, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
	return getgrgid_r(gid, grp, buf, buflen, result);
}
*/
import "C"

// Must run inside the SDK chroot, easiest to just assemble a script to do it
const (
	safePath   = "PATH=/usr/sbin:/usr/bin:/sbin:/bin"
	sudoPrompt = "--prompt=Sudo password for %p: "
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

echo Adding user {{printf "%q" .Username}}
useradd -o -g {{.Gid}} -u {{.Uid}} -s /bin/bash -m \
	-c {{printf "%q" .Name}} {{printf "%q" .Username}}

for g in kvm portage; do
	# copy system group from /usr to /etc if needed
	if getent -s usrfiles group "$g" >/dev/null && \
	   ! getent -s files group "$g" >/dev/null; then
		getent -s usrfiles group "$g" >> /etc/group
	fi
	gpasswd -a {{printf "%q" .Username}} "$g"
done

echo Setting up sudoers
cat >/etc/sudoers.d/90_cros <<EOF
Defaults env_keep += "\
GIT_AUTHOR_EMAIL GIT_AUTHOR_NAME \
GIT_COMMITTER_EMAIL GIT_COMMITTER_NAME \
GIT_PROXY_COMMAND GIT_SSH RSYNC_PROXY \
GPG_AGENT_INFO SSH_AGENT_PID SSH_AUTH_SOCK \
USE FEATURES PORTAGE_USERNAME \
all_proxy ftp_proxy http_proxy https_proxy no_proxy"

{{.Username}} ALL=NOPASSWD: ALL
EOF
chmod 0440 /etc/sudoers.d/90_cros
`
)

var scriptTemplate = template.Must(template.New("script").Parse(script))

type userAndGroup struct {
	*user.User
	Groupname string
}

// Because Go is like... naaaaa, no groups aren't a thing!
// Based on Go's src/os/user/lookup_unix.go
func currentUserAndGroup() (*userAndGroup, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}

	var grp C.struct_group
	var result *C.struct_group
	buflen := C.sysconf(C._SC_GETPW_R_SIZE_MAX)
	if buflen <= 0 || buflen > 1<<20 {
		return nil, fmt.Errorf("unreasonable _SC_GETGR_R_SIZE_MAX of %d", buflen)
	}
	buf := C.malloc(C.size_t(buflen))
	defer C.free(buf)

	r := C.mygetgrgid_r(C.gid_t(gid), &grp,
		(*C.char)(buf),
		C.size_t(buflen),
		&result)
	if r != 0 {
		return nil, fmt.Errorf("lookup gid %d: %s", gid, syscall.Errno(r))
	}
	if result == nil {
		return nil, fmt.Errorf("lookup gid %d failed", gid)
	}

	return &userAndGroup{
		User:      u,
		Groupname: C.GoString(grp.gr_name),
	}, nil
}

func Setup(name string) error {
	chroot := filepath.Join(RepoRoot(), name)
	ug, err := currentUserAndGroup()
	if err != nil {
		return err
	}

	var sc bytes.Buffer
	if err := scriptTemplate.Execute(&sc, ug); err != nil {
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
		unzip.Process.Kill()
		unzip.Wait()
		return err
	}

	if err := unzip.Wait(); err != nil {
		untar.Process.Kill()
		untar.Wait()
		return err
	}

	if err := untar.Wait(); err != nil {
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

	tar := filepath.Join(RepoCache(), "sdk", TarballName(version))
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
