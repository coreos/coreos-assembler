// Copyright 2019 Red Hat
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

package platform

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/coreos/go-semver/semver"

	"github.com/coreos/mantle/system/exec"
)

type MachineOptions struct {
	AdditionalDisks []Disk
}

type Disk struct {
	Size        string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed. Incompatible with BackingFile
	BackingFile string   // raw disk image to use. Incompatible with Size.
	DeviceOpts  []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
	ConfPath    string   // path to ignition to be able to use it with guestfs for temporary qcow2 images
}

var (
	ErrNeedSizeOrFile  = errors.New("Disks need either Size or BackingFile specified")
	ErrBothSizeAndFile = errors.New("Only one of Size and BackingFile can be specified")
	primaryDiskOptions = []string{"serial=primary-disk"}
)

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
		return setupDisk(d.ConfPath, d.Size)
	} else {
		return setupDiskFromFile(d.BackingFile, d.ConfPath)
	}
}

// Create a nameless temporary qcow2 image file backed by a raw image.
func setupDiskFromFile(imageFile string, confPath string) (*os.File, error) {
	// a relative path would be interpreted relative to /tmp
	backingFile, err := filepath.Abs(imageFile)
	if err != nil {
		return nil, err
	}
	// Keep the COW image from breaking if the "latest" symlink changes.
	// Ignore /proc/*/fd/* paths, since they look like symlinks but
	// really aren't.
	if !strings.HasPrefix(backingFile, "/proc/") {
		backingFile, err = filepath.EvalSymlinks(backingFile)
		if err != nil {
			return nil, err
		}
	}

	qcowOpts := fmt.Sprintf("backing_file=%s,lazy_refcounts=on", backingFile)
	return setupDisk(confPath, "-o", qcowOpts)
}

func setupDisk(confPath string, additionalOptions ...string) (*os.File, error) {
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
	if len(confPath) > 0 {
		if err = setupIgnition(confPath, dstFileName); err != nil {
			return nil, fmt.Errorf("ignition injection with guestfs failed: %v", err)
		}
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

func CreateQEMUCommand(board, uuid, biosImage, consolePath, confPath, diskImagePath string, isIgnition bool, options MachineOptions) ([]string, []*os.File, error) {
	var qmCmd []string

	// As we expand this list of supported native + board
	// archs combos we should coordinate with the
	// coreos-assembler folks as they utilize something
	// similar in cosa run
	var qmBinary string
	combo := runtime.GOARCH + "--" + board
	switch combo {
	case "amd64--amd64-usr":
		qmBinary = "qemu-system-x86_64"
		qmCmd = []string{
			"qemu-system-x86_64",
			"-machine", "accel=kvm",
			"-cpu", "host",
			"-m", "1024",
		}
	case "amd64--arm64-usr":
		qmBinary = "qemu-system-aarch64"
		qmCmd = []string{
			"qemu-system-aarch64",
			"-machine", "virt",
			"-cpu", "cortex-a57",
			"-m", "2048",
		}
	case "arm64--amd64-usr":
		qmBinary = "qemu-system-x86_64"
		qmCmd = []string{
			"qemu-system-x86_64",
			"-machine", "pc-q35-2.8",
			"-cpu", "kvm64",
			"-m", "1024",
		}
	case "arm64--arm64-usr":
		qmBinary = "qemu-system-aarch64"
		qmCmd = []string{
			"qemu-system-aarch64",
			"-machine", "virt,accel=kvm,gic-version=3",
			"-cpu", "host",
			"-m", "2048",
		}
	case "s390x--s390x-usr":
		qmBinary = "qemu-system-s390x"
		qmCmd = []string{
			"qemu-system-s390x",
			"-machine", "s390-ccw-virtio,accel=kvm",
			"-cpu", "host",
			"-m", "2048",
		}
	case "ppc64le--ppc64le-usr":
		qmBinary = "qemu-system-ppc64"
		qmCmd = []string{
			"qemu-system-ppc64",
			"-machine", "pseries,accel=kvm,kvm-type=HV",
			"-m", "2048",
		}
	default:
		panic("host-guest combo not supported: " + combo)
	}

	qmCmd = append(qmCmd,
		"-smp", "1",
		"-uuid", uuid,
		"-display", "none",
		"-chardev", "file,id=log,path="+consolePath,
		"-serial", "chardev:log",
		"-object", "rng-random,filename=/dev/urandom,id=rng0",
		"-device", Virtio(board, "rng", "rng=rng0"),
	)

	if board != "s390x-usr" && board != "ppc64le-usr" {
		qmCmd = append(qmCmd, "-bios", biosImage)
	}

	if isIgnition {
		// -fw_cfg is not supported for s390x, instead guestfs utility is used
		if board != "s390x-usr" && board != "ppc64le-usr" {
			qmCmd = append(qmCmd,
				"-fw_cfg", "name=opt/com.coreos/config,file="+confPath)
		}
	} else {
		qmCmd = append(qmCmd,
			"-fsdev", "local,id=cfg,security_model=none,readonly,path="+confPath,
			"-device", Virtio(board, "9p", "fsdev=cfg,mount_tag=config-2"))
	}

	// auto-read-only is only available in 3.1.0 & greater versions of QEMU
	var autoReadOnly string
	version, err := exec.Command(qmBinary, "--version").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving qemu version: %v", err)
	}
	pat := regexp.MustCompile(`version (\d+\.\d+\.\d+)`)
	vNum := pat.FindSubmatch(version)
	if len(vNum) < 2 {
		return nil, nil, fmt.Errorf("unable to parse qemu version number")
	}
	qmSemver, err := semver.NewVersion(string(vNum[1]))
	if err != nil {
		return nil, nil, fmt.Errorf("parsing qemu semver: %v", err)
	}
	if !qmSemver.LessThan(*semver.New("3.1.0")) {
		autoReadOnly = ",auto-read-only=off"
		plog.Debugf("disabling auto-read-only for QEMU drives")
	}

	primaryDisk := Disk{
		BackingFile: diskImagePath,
		DeviceOpts:  primaryDiskOptions,
		ConfPath:    "",
	}

	if board == "s390x-usr" || board == "ppc64le-usr" {
		primaryDisk.ConfPath = confPath
	}

	allDisks := append([]Disk{primaryDisk}, options.AdditionalDisks...)

	var extraFiles []*os.File
	fdnum := 3 // first additional file starts at position 3
	fdset := 1

	for _, disk := range allDisks {
		optionsDiskFile, err := disk.setupFile()
		if err != nil {
			return nil, nil, err
		}
		//defer optionsDiskFile.Close()
		extraFiles = append(extraFiles, optionsDiskFile)

		id := fmt.Sprintf("d%d", fdnum)
		qmCmd = append(qmCmd, "-add-fd", fmt.Sprintf("fd=%d,set=%d", fdnum, fdset),
			"-drive", fmt.Sprintf("if=none,id=%s,format=qcow2,file=/dev/fdset/%d%s", id, fdset, autoReadOnly),
			"-device", Virtio(board, "blk", fmt.Sprintf("drive=%s%s", id, disk.getOpts())))
		fdnum += 1
		fdset += 1
	}

	return qmCmd, extraFiles, nil
}

// The virtio device name differs between machine types but otherwise
// configuration is the same. Use this to help construct device args.
func Virtio(board, device, args string) string {
	var suffix string
	switch board {
	case "amd64-usr", "ppc64le-usr":
		suffix = "pci"
	case "arm64-usr":
		suffix = "device"
	case "s390x-usr":
		suffix = "ccw"
	default:
		panic(board)
	}
	return fmt.Sprintf("virtio-%s-%s,%s", device, suffix, args)
}

// Note: This is misleading. We are NOT putting the ignition config in the root parition. We mount the boot partition on / just to get around the fact that
// the root partition does not need to be mounted to inject ignition config. Now that we have LUKS , we have to do more work to detect a LUKS root partition
// and it is not needed here.
const fileRemoteLocation = "/ignition/config.ign"

// setupIgnition copies the ignition file inside the disk image.
func setupIgnition(confPath string, diskImagePath string) error {
	// Set guestfish backend to direct in order to avoid libvirt as backend.
	// Using libvirt can lead to permission denied issues if it does not have access
	// rights to the qcow image
	os.Setenv("LIBGUESTFS_BACKEND", "direct")
	cmd := exec.Command("guestfish", "--listen", "-a", diskImagePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %v", err)
	}
	defer stdout.Close()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running guestfish: %v", err)
	}
	buf, err := ioutil.ReadAll(stdout)
	if err != nil {
		return fmt.Errorf("reading guestfish output: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("waiting for guestfish response: %v", err)
	}
	//GUESTFISH_PID=$PID; export GUESTFISH_PID
	gfVarPid := strings.Split(string(buf), ";")
	if len(gfVarPid) != 2 {
		return fmt.Errorf("Failing parsing GUESTFISH_PID got: expecting length 2 got instead %d", len(gfVarPid))
	}
	gfVarPidArr := strings.Split(gfVarPid[0], "=")
	if len(gfVarPidArr) != 2 {
		return fmt.Errorf("Failing parsing GUESTFISH_PID got: expecting length 2 got instead %d", len(gfVarPid))
	}
	pid := gfVarPidArr[1]
	remote := fmt.Sprintf("--remote=%s", pid)

	defer func() {
		plog.Debugf("guestfish exit (PID:%s)", pid)
		if err := exec.Command("guestfish", remote, "exit").Run(); err != nil {
			plog.Errorf("guestfish exit failed: %v", err)
		}
	}()

	if err := exec.Command("guestfish", remote, "run").Run(); err != nil {
		return fmt.Errorf("guestfish launch failed: %v", err)
	}

	bootfs, err := findLabel("boot", pid)
	if err != nil {
		return fmt.Errorf("guestfish command failed to find boot label: %v", err)
	}

	if err := exec.Command("guestfish", remote, "mount", bootfs, "/").Run(); err != nil {
		return fmt.Errorf("guestfish boot mount failed: %v", err)
	}

	if err := exec.Command("guestfish", remote, "mkdir-p", "/ignition").Run(); err != nil {
		return fmt.Errorf("guestfish directory creation failed: %v", err)
	}

	if err := exec.Command("guestfish", remote, "upload", confPath, fileRemoteLocation).Run(); err != nil {
		return fmt.Errorf("guestfish upload failed: %v", err)
	}

	if err := exec.Command("guestfish", remote, "umount-all").Run(); err != nil {
		return fmt.Errorf("guestfish umount failed: %v", err)
	}
	return nil
}

// findLabel finds the partition based on the label. The partition belongs to the image attached to the guestfish instance identified by pid.
func findLabel(label, pid string) (string, error) {
	if pid == "" {
		return "", fmt.Errorf("The pid cannot be empty")
	}
	if label == "" {
		return "", fmt.Errorf("The label cannot be empty")
	}
	remote := fmt.Sprintf("--remote=%s", pid)
	cmd := exec.Command("guestfish", remote, "findfs-label", label)
	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get stdout for findfs-label failed: %v", err)
	}
	return strings.TrimSpace(string(stdout)), nil
}
