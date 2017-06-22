// Copyright 2017 CoreOS, Inc.
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

package qemu

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
)

// Copy input image to output and specialize output for running kola tests.
// This is not mandatory; the tests will do their best without it.
func MakeDiskTemplate(inputPath, outputPath string) (result error) {
	seterr := func(err error) {
		if result == nil {
			result = err
		}
	}

	// copy file
	// cp is used since it supports sparse and reflink.
	cp := exec.Command("cp", "--force",
		"--sparse=always", "--reflink=auto",
		inputPath, outputPath)
	cp.Stdout = os.Stdout
	cp.Stderr = os.Stderr
	if err := cp.Run(); err != nil {
		return fmt.Errorf("copying file: %v", err)
	}
	defer func() {
		if result != nil {
			os.Remove(outputPath)
		}
	}()

	// create mount point
	tmpdir, err := ioutil.TempDir("", "kola-qemu-")
	if err != nil {
		return fmt.Errorf("making temporary directory: %v", err)
	}
	defer func() {
		if err := os.Remove(tmpdir); err != nil {
			seterr(fmt.Errorf("deleting directory %s: %v", tmpdir, err))
		}
	}()

	// set up partitions
	cmd := exec.Command("kpartx", "-av", outputPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %v", err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running kpartx: %v", err)
	}
	buf, err := ioutil.ReadAll(stdout)
	if err != nil {
		cmd.Wait()
		return fmt.Errorf("reading kpartx output: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("setting up partitions: %v", err)
	}
	defer func() {
		if err := exec.Command("kpartx", "-d", outputPath).Run(); err != nil {
			seterr(fmt.Errorf("tearing down partitions: %v", err))
		}
	}()

	// extract loop device name
	match := regexp.MustCompile(" (loop[0-9]+)p[0-9]+ ").FindStringSubmatch(string(buf))
	if match == nil {
		return fmt.Errorf("couldn't obtain loop device name")
	}
	loopnode := match[1]

	// wait for OEM block device
	mapperNode := "/dev/mapper/" + loopnode + "p6"
	err = util.Retry(1000, 5*time.Millisecond, func() error {
		if _, err := os.Stat(mapperNode); !os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("timed out waiting for device node")
	})
	if err != nil {
		return err
	}

	// mount OEM partition
	if err := exec.Command("mount", mapperNode, tmpdir).Run(); err != nil {
		return fmt.Errorf("mounting OEM partition %s on %s: %v", mapperNode, tmpdir, err)
	}
	defer func() {
		if err := exec.Command("umount", tmpdir).Run(); err != nil {
			seterr(fmt.Errorf("unmounting %s: %v", tmpdir, err))
		}
	}()

	// write console settings to grub.cfg
	f, err := os.OpenFile(filepath.Join(tmpdir, "grub.cfg"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening grub.cfg: %v", err)
	}
	defer f.Close()
	if _, err = f.WriteString("set linux_console=\"console=ttyS0,115200\"\n"); err != nil {
		return fmt.Errorf("writing grub.cfg: %v", err)
	}

	return
}
