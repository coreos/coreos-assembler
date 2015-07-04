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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	sudoPrompt = "--prompt=Sudo password for %p: "
)

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
