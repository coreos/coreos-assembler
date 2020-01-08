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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"
)

type MachineOptions struct {
	AdditionalDisks []Disk
}

type Disk struct {
	Size        string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed. Incompatible with BackingFile
	BackingFile string   // raw disk image to use. Incompatible with Size.
	Channel     string   // virtio (default), nvme
	DeviceOpts  []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
}

type QemuInstance struct {
	qemu      exec.Cmd
	swtpmTmpd string
	swtpm     exec.Cmd
}

func (inst *QemuInstance) Pid() int {
	return inst.qemu.Pid()
}

// parse /proc/net/tcp to determine the port selected by QEMU
func (inst *QemuInstance) SSHAddress() (string, error) {
	pid := fmt.Sprintf("%d", inst.Pid())
	data, err := ioutil.ReadFile("/proc/net/tcp")
	if err != nil {
		return "", errors.Wrap(err, "reading /proc/net/tcp")
	}

	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			// at least 10 fields are neeeded for the local & remote address and the inode
			continue
		}
		localAddress := fields[1]
		remoteAddress := fields[2]
		inode := fields[9]

		var isLocalPat *regexp.Regexp
		if util.HostEndianness == util.LITTLE {
			isLocalPat = regexp.MustCompile("0100007F:[[:xdigit:]]{4}")
		} else {
			isLocalPat = regexp.MustCompile("7F000001:[[:xdigit:]]{4}")
		}

		if !isLocalPat.MatchString(localAddress) || remoteAddress != "00000000:0000" {
			continue
		}

		dir := fmt.Sprintf("/proc/%s/fd/", pid)
		fds, err := ioutil.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("listing %s: %v", dir, err)
		}

		for _, f := range fds {
			link, err := os.Readlink(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			socketPattern := regexp.MustCompile("socket:\\[([0-9]+)\\]")
			match := socketPattern.FindStringSubmatch(link)
			if len(match) > 1 {
				if inode == match[1] {
					// this entry belongs to the QEMU pid, parse the port and return the address
					portHex := strings.Split(localAddress, ":")[1]
					port, err := strconv.ParseInt(portHex, 16, 32)
					if err != nil {
						return "", errors.Wrapf(err, "decoding port %q", portHex)
					}
					return fmt.Sprintf("127.0.0.1:%d", port), nil
				}
			}
		}
	}
	return "", fmt.Errorf("didn't find an address")
}

func (inst *QemuInstance) Wait() error {
	return inst.qemu.Wait()
}

func (inst *QemuInstance) Destroy() {
	if inst.qemu != nil {
		if err := inst.qemu.Kill(); err != nil {
			plog.Errorf("Error killing qemu instance %v: %v", inst.Pid(), err)
		}
		inst.qemu.Wait() // Ignore errors
	}
	if inst.swtpmTmpd != "" {
		if inst.swtpm != nil {
			inst.swtpm.Kill() // Ignore errors
		}
		// And ensure it's cleaned up
		inst.swtpm.Wait()
		if err := os.RemoveAll(inst.swtpmTmpd); err != nil {
			plog.Errorf("Error removing swtpm dir: %v", err)
		}
	}
}

// QemuBuilder is a configurator that can then create a qemu instance
type QemuBuilder struct {
	Board string

	// Config is a path to Ignition configuration
	Config string

	Memory     int
	Processors int
	Uuid       string
	Firmware   string
	Swtpm      bool
	Pdeathsig  bool
	Argv       []string

	InheritConsole bool

	primaryDiskAdded bool

	finalized bool
	diskId    uint
	fds       []*os.File
}

func NewBuilder(board, config string) *QemuBuilder {
	ret := QemuBuilder{
		Board:     board,
		Config:    config,
		Firmware:  "bios",
		Swtpm:     true,
		Pdeathsig: true,
		Argv:      []string{},
	}
	return &ret
}

// AddFd appends a file descriptor that will be passed to qemu,
// returning a "/dev/fdset/<num>" argument that one can use with e.g.
// -drive file=/dev/fdset/<num>.
func (builder *QemuBuilder) AddFd(fd *os.File) string {
	set := len(builder.fds) + 1
	builder.fds = append(builder.fds, fd)
	return fmt.Sprintf("/dev/fdset/%d", set)
}

// virtio returns a virtio device argument for qemu, which is architecture dependent
func virtio(board, device, args string) string {
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

// addQcow2Disk adds a disk image from a file descriptor,
// mounted read-write, formatted qcow2.
func (builder *QemuBuilder) addQcow2DiskFd(fd *os.File, channel string, options []string) {
	opts := ""
	if len(options) > 0 {
		opts = "," + strings.Join(options, ",")
	}
	fdset := builder.AddFd(fd)
	id := fmt.Sprintf("d%d", builder.diskId)
	builder.diskId += 1
	switch channel {
	case "virtio":
		builder.Append("-device", virtio(builder.Board, "blk", fmt.Sprintf("drive=%s%s", id, opts)))
	case "nvme":
		builder.Append("-device", fmt.Sprintf("nvme,drive=%s%s", id, opts))
	default:
		panic(fmt.Sprintf("Unhandled channel: %s", channel))
	}
	builder.Append("-drive", fmt.Sprintf("if=none,id=%s,format=qcow2,file=%s,auto-read-only=off", id, fdset))
}

func (builder *QemuBuilder) ConsoleToFile(path string) {
	builder.Append("-display", "none", "-chardev", "file,id=log,path="+path, "-serial", "chardev:log")
}

func (builder *QemuBuilder) EnableUsermodeNetworking(forwardedPort uint) {
	var forward string
	if forwardedPort != 0 {
		forward = fmt.Sprintf(",hostfwd=tcp:127.0.0.1:0-:%d", forwardedPort)
	}
	builder.Append("-netdev", "user,id=eth0"+forward, "-device", virtio(builder.Board, "net", "netdev=eth0"))
}

// supportsFwCfg if the target system supports injecting
// Ignition via the qemu -fw_cfg option.
func (builder *QemuBuilder) supportsFwCfg() bool {
	switch builder.Board {
	case "s390x-usr", "ppc64le-usr":
		return false
	}
	return true
}

// supportsSwtpm if the target system supports a virtual TPM device
func (builder *QemuBuilder) supportsSwtpm() bool {
	// Yes, this is the same as supportsFwCfg *currently* but
	// might not be in the future.
	switch builder.Board {
	case "s390x-usr", "ppc64le-usr":
		return false
	}
	return true
}

// fileRemoteLocation is a bit misleading. We are NOT putting the ignition config in the root parition. We mount the boot partition on / just to get around the fact that
// the root partition does not need to be mounted to inject ignition config. Now that we have LUKS , we have to do more work to detect a LUKS root partition
// and it is not needed here.
const fileRemoteLocation = "/ignition/config.ign"

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

func resolveBackingFile(backingFile string) (string, error) {
	backingFile, err := filepath.Abs(backingFile)
	if err != nil {
		return "", err
	}
	// Keep the COW image from breaking if the "latest" symlink changes.
	// Ignore /proc/*/fd/* paths, since they look like symlinks but
	// really aren't.
	if !strings.HasPrefix(backingFile, "/proc/") {
		backingFile, err = filepath.EvalSymlinks(backingFile)
		if err != nil {
			return "", err
		}
	}
	return backingFile, nil
}

func mkpath(basedir string) (string, error) {
	f, err := ioutil.TempFile(basedir, "mantle-qemu")
	if err != nil {
		return "", err
	}
	defer f.Close()
	return f.Name(), nil
}

func (builder *QemuBuilder) addDiskImpl(disk *Disk, primary bool) error {
	dstFileName, err := mkpath("")
	if err != nil {
		return err
	}
	defer os.Remove(dstFileName)

	imgOpts := []string{"create", "-f", "qcow2", dstFileName}
	if disk.BackingFile != "" {
		backingFile, err := resolveBackingFile(disk.BackingFile)
		if err != nil {
			return err
		}
		imgOpts = append(imgOpts, "-o", fmt.Sprintf("backing_file=%s,lazy_refcounts=on", backingFile))
	}
	if disk.Size != "" {
		imgOpts = append(imgOpts, disk.Size)
	}
	qemuImg := exec.Command("qemu-img", imgOpts...)
	qemuImg.Stderr = os.Stderr

	if err := qemuImg.Run(); err != nil {
		return err
	}
	// If the board doesn't support -fw_cfg, inject via libguestfs on the
	// primary disk.
	if primary && builder.Config != "" && !builder.supportsFwCfg() {
		if err = setupIgnition(builder.Config, dstFileName); err != nil {
			return fmt.Errorf("ignition injection with guestfs failed: %v", err)
		}
	}
	fd, err := os.OpenFile(dstFileName, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	diskOpts := disk.DeviceOpts
	if primary {
		diskOpts = append(diskOpts, "serial=primary-disk")
	}
	channel := disk.Channel
	if channel == "" {
		channel = "virtio"
	}
	builder.addQcow2DiskFd(fd, channel, diskOpts)
	return nil
}

func (builder *QemuBuilder) AddPrimaryDisk(disk *Disk) error {
	if builder.primaryDiskAdded {
		panic("Multiple primary disks specified")
	}
	builder.primaryDiskAdded = true
	return builder.addDiskImpl(disk, true)
}

func (builder *QemuBuilder) AddDisk(disk *Disk) error {
	return builder.addDiskImpl(disk, false)
}

func (builder *QemuBuilder) finalize() {
	if builder.finalized {
		return
	}
	if builder.Processors == 0 {
		builder.Processors = 1
	}
	if builder.Memory == 0 {
		// FIXME; Required memory should really be a property of the tests, and
		// let's try to drop these arch-specific overrides.  ARM was bumped via
		// commit 09391907c0b25726374004669fa6c2b161e3892f
		// Commit:     Geoff Levand <geoff@infradead.org>
		// CommitDate: Mon Aug 21 12:39:34 2017 -0700
		//
		// kola: More memory for arm64 qemu guest machines
		//
		// arm64 guest machines seem to run out of memory with 1024 MiB of
		// RAM, so increase to 2048 MiB.

		// Then later, other non-x86_64 seemed to just copy that.
		memory := 1024
		switch builder.Board {
		case "arm64-usr":
		case "s390x-usr":
		case "ppc64le-usr":
			memory = 2048
		}
		builder.Memory = memory
	}
	builder.finalized = true
}

func (builder *QemuBuilder) Append(args ...string) {
	builder.Argv = append(builder.Argv, args...)
}

// baseQemuArgs takes a board and returns the basic qemu
// arguments needed for the current architecture.
func baseQemuArgs(board string) []string {
	combo := runtime.GOARCH + "--" + board
	switch combo {
	case "amd64--amd64-usr":
		return []string{
			"qemu-system-x86_64",
			"-machine", "accel=kvm",
			"-cpu", "host",
		}
	case "amd64--arm64-usr":
		return []string{
			"qemu-system-aarch64",
			"-machine", "virt",
			"-cpu", "cortex-a57",
		}
	case "arm64--arm64-usr":
		return []string{
			"qemu-system-aarch64",
			"-machine", "virt,accel=kvm,gic-version=3",
			"-cpu", "host",
		}
	case "s390x--s390x-usr":
		return []string{
			"qemu-system-s390x",
			"-machine", "s390-ccw-virtio,accel=kvm",
			"-cpu", "host",
		}
	case "ppc64le--ppc64le-usr":
		return []string{
			"qemu-system-ppc64",
			"-machine", "pseries,accel=kvm,kvm-type=HV",
		}
	default:
		panic("host-guest combo not supported: " + combo)
	}
}

func (builder *QemuBuilder) setupUefi(secureBoot bool) error {
	varsVariant := ""
	if secureBoot {
		varsVariant = ".secboot"
	}
	varsSrc, err := os.Open(fmt.Sprintf("/usr/share/edk2/ovmf/OVMF_VARS%s.fd", varsVariant))
	if err != nil {
		return err
	}
	defer varsSrc.Close()
	vars, err := ioutil.TempFile("", "mantle-qemu")
	if err != nil {
		return err
	}
	if _, err := io.Copy(vars, varsSrc); err != nil {
		return err
	}
	_, err = vars.Seek(0, 0)
	if err != nil {
		return err
	}

	fdset := builder.AddFd(vars)
	builder.Append("-drive", fmt.Sprintf("file=/usr/share/edk2/ovmf/OVMF_CODE%s.fd,if=pflash,format=raw,unit=0,readonly=on,auto-read-only=off", varsVariant))
	builder.Append("-drive", fmt.Sprintf("file=%s,if=pflash,format=raw,unit=1,readonly=off,auto-read-only=off", fdset))
	builder.Append("-machine", "q35")
	return nil
}

func (builder *QemuBuilder) Exec() (*QemuInstance, error) {
	builder.finalize()
	var err error

	inst := QemuInstance{}

	argv := baseQemuArgs(builder.Board)
	argv = append(argv, "-m", fmt.Sprintf("%d", builder.Memory))

	switch builder.Firmware {
	case "bios":
		break
	case "uefi":
		builder.setupUefi(false)
	case "uefi-secure":
		builder.setupUefi(true)
		break
	default:
		panic(fmt.Sprintf("Unknown firmware: %s", builder.Firmware))
	}

	// We always provide a random source
	argv = append(argv, "-object", "rng-random,filename=/dev/urandom,id=rng0",
		"-device", virtio(builder.Board, "rng", "rng=rng0"))
	if builder.Uuid != "" {
		argv = append(argv, "-uuid", builder.Uuid)
	}

	// We never want a popup window
	argv = append(argv, "-nographic")

	// Handle Ignition
	if builder.Config != "" {
		if builder.supportsFwCfg() {
			builder.Append("-fw_cfg", "name=opt/com.coreos/config,file="+builder.Config)
		} else if !builder.primaryDiskAdded {
			// Otherwise, we should have handled it in builder.AddPrimaryDisk
			panic("Ignition specified but no primary disk")
		}
	}

	if builder.Swtpm && builder.supportsSwtpm() {
		inst.swtpmTmpd, err = ioutil.TempDir("", "kola-swtpm")
		if err != nil {
			return nil, err
		}

		swtpmSock := filepath.Join(inst.swtpmTmpd, "swtpm-sock")

		inst.swtpm = exec.Command("swtpm", "socket", "--tpm2",
			"--ctrl", fmt.Sprintf("type=unixio,path=%s", swtpmSock),
			"--terminate", "--tpmstate", fmt.Sprintf("dir=%s", inst.swtpmTmpd))
		cmd := inst.swtpm.(*exec.ExecCmd)
		// For now silence the swtpm stderr as it prints errors when
		// disconnected, but that's normal.
		if builder.Pdeathsig {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Pdeathsig: syscall.SIGTERM,
			}
		}
		if err = inst.swtpm.Start(); err != nil {
			return nil, err
		}
		argv = append(argv, "-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s", swtpmSock),
			"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-tis,tpmdev=tpm0")
	}

	fdnum := 3 // first additional file starts at position 3
	for i, _ := range builder.fds {
		fdset := i + 1 // Start at 1
		argv = append(argv, "-add-fd", fmt.Sprintf("fd=%d,set=%d", fdnum, fdset))
		fdnum += 1
	}

	// And the custom arguments
	argv = append(argv, builder.Argv...)

	inst.qemu = exec.Command(argv[0], argv[1:]...)

	cmd := inst.qemu.(*exec.ExecCmd)
	cmd.Stderr = os.Stderr

	if builder.Pdeathsig {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Pdeathsig: syscall.SIGTERM,
		}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, builder.fds...)

	if builder.InheritConsole {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err = inst.qemu.Start(); err != nil {
		return nil, err
	}

	return &inst, nil
}

func (builder *QemuBuilder) Close() {
	if builder.fds == nil {
		return
	}
	for _, f := range builder.fds {
		f.Close()
	}
	builder.fds = nil
}
