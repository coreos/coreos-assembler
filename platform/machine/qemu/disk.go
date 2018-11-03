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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
)

type Disk struct {
	Size        string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed. Incompatible with BackingFile
	BackingFile string   // raw disk image to use. Incompatible with Size.
	DeviceOpts  []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
}

var (
	ErrNeedSizeOrFile  = errors.New("Disks need either Size or BackingFile specified")
	ErrBothSizeAndFile = errors.New("Only one of Size and BackingFile can be specified")
	primaryDiskOptions = []string{"serial=primary-disk"}
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

	// set up loop device
	cmd := exec.Command("losetup", "-Pf", "--show", outputPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %v", err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running losetup: %v", err)
	}
	buf, err := ioutil.ReadAll(stdout)
	if err != nil {
		cmd.Wait()
		return fmt.Errorf("reading losetup output: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("setting up loop device: %v", err)
	}
	loopdev := strings.TrimSpace(string(buf))
	defer func() {
		if err := exec.Command("losetup", "-d", loopdev).Run(); err != nil {
			seterr(fmt.Errorf("tearing down loop device: %v", err))
		}
	}()

	// wait for OEM block device
	oemdev := loopdev + "p6"
	err = util.Retry(1000, 5*time.Millisecond, func() error {
		if _, err := os.Stat(oemdev); !os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("timed out waiting for device node; did you specify a qcow image by mistake?")
	})
	if err != nil {
		return err
	}

	// mount OEM partition
	if err := exec.Command("mount", oemdev, tmpdir).Run(); err != nil {
		return fmt.Errorf("mounting OEM partition %s on %s: %v", oemdev, tmpdir, err)
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

func (d Disk) getOpts() string {
	if len(d.DeviceOpts) == 0 {
		return ""
	}
	return "," + strings.Join(d.DeviceOpts, ",")
}

func (d Disk) setupFile() (*os.File, error) {
	if d.Size == "" && d.BackingFile == "" {
		return nil, ErrNeedSizeOrFile
	}
	if d.Size != "" && d.BackingFile != "" {
		return nil, ErrBothSizeAndFile
	}

	if d.Size != "" {
		return setupDisk(d.Size)
	} else {
		return setupDiskFromFile(d.BackingFile)
	}
}

// Create a nameless temporary qcow2 image file backed by a raw image.
func setupDiskFromFile(imageFile string) (*os.File, error) {
	// a relative path would be interpreted relative to /tmp
	backingFile, err := filepath.Abs(imageFile)
	if err != nil {
		return nil, err
	}
	// keep the COW image from breaking if the "latest" symlink changes
	backingFile, err = filepath.EvalSymlinks(backingFile)
	if err != nil {
		return nil, err
	}

	qcowOpts := fmt.Sprintf("backing_file=%s,lazy_refcounts=on", backingFile)
	return setupDisk("-o", qcowOpts)
}

func setupDisk(additionalOptions ...string) (*os.File, error) {
	dstFileName, err := mkpath("")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dstFileName)

	opts := []string{"create", "-f", "qcow2", dstFileName}
	opts = append(opts, additionalOptions...)

	qemuImg := exec.Command("qemu-img", opts...)
	qemuImg.Stderr = os.Stderr

	if err := qemuImg.Run(); err != nil {
		return nil, err
	}

	return os.OpenFile(dstFileName, os.O_RDWR, 0)
}

func mkpath(basedir string) (string, error) {
	f, err := ioutil.TempFile(basedir, "mantle-qemu")
	if err != nil {
		return "", err
	}
	defer f.Close()
	return f.Name(), nil
}
