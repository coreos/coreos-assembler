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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	ignconverter "github.com/coreos/ign-converter"
	v3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"
)

type MachineOptions struct {
	AdditionalDisks []Disk
}

type Disk struct {
	Size          string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed. Incompatible with BackingFile
	BackingFile   string   // raw disk image to use. Incompatible with Size.
	Channel       string   // virtio (default), nvme
	DeviceOpts    []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
	SectorSize    int      // if not 0, override disk sector size
	MultiPathDisk bool     // if true, present multiple paths
}

type QemuInstance struct {
	qemu      exec.Cmd
	tmpConfig string
	swtpmTmpd string
	swtpm     exec.Cmd
}

// AttachFormat returns the Qemu format that should be used
func (d *Disk) AttachFormat() string {
	// qcow2 locking defeats our multipathing right now, see below
	if d.MultiPathDisk {
		return "raw"
	}
	return "qcow2"
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
	if inst.tmpConfig != "" {
		os.Remove(inst.tmpConfig)
	}
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
	// ConfigFile is a path to Ignition configuration
	ConfigFile string
	// ForceConfigInjection is useful for booting `metal` images directly
	ForceConfigInjection bool
	configInjected       bool

	// Memory defaults to 1024 on most architectures, others it may be 2048
	Memory int
	// Processors < 0 means to use host count, unset means 1, values > 1 are directly used
	Processors int
	Uuid       string
	Firmware   string
	Swtpm      bool
	Pdeathsig  bool
	Argv       []string

	// AppendKernelArguments are appended to the bootloader config
	AppendKernelArguments string

	// IgnitionNetworkKargs are written to /boot/ignition
	IgnitionNetworkKargs string

	Hostname string

	InheritConsole bool

	primaryDiskAdded bool

	MultiPathDisk bool

	// cleanupConfig is true if we own the Ignition config
	cleanupConfig bool

	finalized bool
	diskId    uint
	fs9pId    uint
	fds       []*os.File
}

func NewBuilder() *QemuBuilder {
	ret := QemuBuilder{
		Firmware:      "bios",
		Swtpm:         true,
		Pdeathsig:     true,
		MultiPathDisk: false,
		Argv:          []string{},
	}
	return &ret
}

func (builder *QemuBuilder) SetConfig(config v3types.Config, convSpec2 bool) error {
	var configBuf []byte
	var err error
	if convSpec2 {
		ignc2, err := ignconverter.Translate3to2(config)
		if err != nil {
			return err
		}
		configBuf, err = json.Marshal(ignc2)
		if err != nil {
			return err
		}
	} else {
		configBuf, err = json.Marshal(config)
		if err != nil {
			return err
		}
	}
	tmpf, err := ioutil.TempFile("", "mantle-qemu-ign")
	if err != nil {
		return err
	}
	if _, err := tmpf.Write(configBuf); err != nil {
		return err
	}

	builder.ConfigFile = tmpf.Name()
	builder.cleanupConfig = true
	return nil
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
func virtio(device, args string) string {
	var suffix string
	switch system.RpmArch() {
	case "x86_64", "ppc64le":
		suffix = "pci"
	case "aarch64":
		suffix = "device"
	case "s390x":
		suffix = "ccw"
	default:
		panic(fmt.Sprintf("RpmArch %s unhandled in virtio()", system.RpmArch()))
	}
	return fmt.Sprintf("virtio-%s-%s,%s", device, suffix, args)
}

// addDisk adds a disk image from a file descriptor,
// mounted read-write, formatted qcow2.
func (builder *QemuBuilder) addDiskFd(fd *os.File, channel string, disk *Disk, options []string) {
	opts := ""
	if len(options) > 0 {
		opts = "," + strings.Join(options, ",")
	}
	fdset := builder.AddFd(fd)
	builder.diskId += 1
	id := fmt.Sprintf("d%d", builder.diskId)

	if disk.MultiPathDisk {
		// Fake a NVME device with a fake WWN. All these attributes are needed in order
		// to trick multipath-tools that this is a "real" multipath device.
		// Each disk is presented on its own controller.

		// The WWN needs to be a unique uint64 number
		rand.Seed(time.Now().UnixNano())
		wwn := rand.Uint64()

		for i := 0; i < 2; i++ {
			if i == 1 {
				opts = strings.Replace(opts, "bootindex=1", "bootindex=2", -1)
			}
			pId := fmt.Sprintf("mpath%d%d", builder.diskId, i)
			scsiId := fmt.Sprintf("scsi_%s", pId)
			builder.Append("-device", fmt.Sprintf("virtio-scsi-pci,id=%s", scsiId))
			builder.Append("-device",
				fmt.Sprintf("scsi-hd,bus=%s.0,drive=%s,vendor=NVME,product=VirtualMultipath,wwn=%d%s",
					scsiId, pId, wwn, opts))
			builder.Append("-drive", fmt.Sprintf("if=none,id=%s,format=%s,file=%s,auto-read-only=off,media=disk",
				pId, disk.AttachFormat(), fdset))
		}
		return
	}

	switch channel {
	case "virtio":
		builder.Append("-device", virtio("blk", fmt.Sprintf("drive=%s%s", id, opts)))
	case "nvme":
		builder.Append("-device", fmt.Sprintf("nvme,drive=%s%s", id, opts))
	default:
		panic(fmt.Sprintf("Unhandled channel: %s", channel))
	}

	builder.Append("-drive", fmt.Sprintf("if=none,id=%s,format=%s,file=%s,auto-read-only=off",
		id, disk.AttachFormat(), fdset))
}

func (builder *QemuBuilder) ConsoleToFile(path string) {
	builder.Append("-display", "none", "-chardev", "file,id=log,path="+path, "-serial", "chardev:log")
}

func (builder *QemuBuilder) EnableUsermodeNetworking(forwardedPort uint) {
	netdev := "user,id=eth0"
	if forwardedPort != 0 {
		netdev += fmt.Sprintf(",hostfwd=tcp:127.0.0.1:0-:%d", forwardedPort)
	}

	if builder.Hostname != "" {
		netdev += fmt.Sprintf(",hostname=%s", builder.Hostname)
	}

	builder.Append("-netdev", netdev, "-device", virtio("net", "netdev=eth0"))
}

// Mount9p sets up a mount point from the host to guest.  To be replaced
// with https://virtio-fs.gitlab.io/ once it lands everywhere.
func (builder *QemuBuilder) Mount9p(source, destHint string, readonly bool) {
	builder.fs9pId += 1
	readonlyStr := ""
	if readonly {
		readonlyStr = ",readonly"
	}
	builder.Append("--fsdev", fmt.Sprintf("local,id=fs%d,path=%s,security_model=mapped%s", builder.fs9pId, source, readonlyStr))
	builder.Append("-device", virtio("9p", fmt.Sprintf("fsdev=fs%d,mount_tag=%s", builder.fs9pId, destHint)))
}

// supportsFwCfg if the target system supports injecting
// Ignition via the qemu -fw_cfg option.
func (builder *QemuBuilder) supportsFwCfg() bool {
	switch system.RpmArch() {
	case "s390x", "ppc64le":
		return false
	}
	return true
}

// supportsSwtpm if the target system supports a virtual TPM device
func (builder *QemuBuilder) supportsSwtpm() bool {
	// Yes, this is the same as supportsFwCfg *currently* but
	// might not be in the future.
	switch system.RpmArch() {
	case "s390x", "ppc64le":
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
		return "", errors.Wrapf(err, "get stdout for findfs-label failed")
	}
	return strings.TrimSpace(string(stdout)), nil
}

type coreosGuestfish struct {
	cmd *exec.ExecCmd

	remote string
}

func newGuestfish(diskImagePath string, diskSectorSize int) (*coreosGuestfish, error) {
	// Set guestfish backend to direct in order to avoid libvirt as backend.
	// Using libvirt can lead to permission denied issues if it does not have access
	// rights to the qcow image
	guestfish_args := []string{"--listen"}
	if diskSectorSize != 0 {
		guestfish_args = append(guestfish_args, fmt.Sprintf("--blocksize=%d", diskSectorSize))
	}
	guestfish_args = append(guestfish_args, "-a", diskImagePath)
	cmd := exec.Command("guestfish", guestfish_args...)
	cmd.Env = append(os.Environ(), "LIBGUESTFS_BACKEND=direct")
	// make sure it inherits stderr so we see any error message
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrapf(err, "getting stdout pipe")
	}
	defer stdout.Close()

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrapf(err, "running guestfish")
	}
	buf, err := ioutil.ReadAll(stdout)
	if err != nil {
		return nil, errors.Wrapf(err, "reading guestfish output")
	}
	if err := cmd.Wait(); err != nil {
		return nil, errors.Wrapf(err, "waiting for guestfish response")
	}
	//GUESTFISH_PID=$PID; export GUESTFISH_PID
	gfVarPid := strings.Split(string(buf), ";")
	if len(gfVarPid) != 2 {
		return nil, fmt.Errorf("Failing parsing GUESTFISH_PID got: expecting length 2 got instead %d", len(gfVarPid))
	}
	gfVarPidArr := strings.Split(gfVarPid[0], "=")
	if len(gfVarPidArr) != 2 {
		return nil, fmt.Errorf("Failing parsing GUESTFISH_PID got: expecting length 2 got instead %d", len(gfVarPid))
	}
	pid := gfVarPidArr[1]
	remote := fmt.Sprintf("--remote=%s", pid)

	if err := exec.Command("guestfish", remote, "run").Run(); err != nil {
		return nil, errors.Wrapf(err, "guestfish launch failed")
	}

	bootfs, err := findLabel("boot", pid)
	if err != nil {
		return nil, errors.Wrapf(err, "guestfish command failed to find boot label")
	}

	if err := exec.Command("guestfish", remote, "mount", bootfs, "/").Run(); err != nil {
		return nil, errors.Wrapf(err, "guestfish boot mount failed")
	}

	return &coreosGuestfish{
		cmd:    cmd,
		remote: remote,
	}, nil
}

func (gf *coreosGuestfish) destroy() {
	if err := exec.Command("guestfish", gf.remote, "exit").Run(); err != nil {
		plog.Errorf("guestfish exit failed: %v", err)
	}
}

// setupPreboot performs changes necessary before the disk is booted
func setupPreboot(confPath, knetargs, kargs string, diskImagePath string, diskSectorSize int) error {
	gf, err := newGuestfish(diskImagePath, diskSectorSize)
	if err != nil {
		return err
	}
	defer gf.destroy()

	if confPath != "" {
		if err := exec.Command("guestfish", gf.remote, "mkdir-p", "/ignition").Run(); err != nil {
			return errors.Wrapf(err, "guestfish directory creation failed")
		}

		if err := exec.Command("guestfish", gf.remote, "upload", confPath, fileRemoteLocation).Run(); err != nil {
			return errors.Wrapf(err, "guestfish upload failed")
		}
	}

	// See /boot/grub2/grub.cfg
	if knetargs != "" {
		grubStr := fmt.Sprintf("set ignition_network_kcmdline='%s'\n", knetargs)
		if err := exec.Command("guestfish", gf.remote, "write", "/ignition.firstboot", grubStr).Run(); err != nil {
			return errors.Wrapf(err, "guestfish write")
		}
	}

	if kargs != "" {
		confpathout, err := exec.Command("guestfish", gf.remote, "glob-expand", "/loader/entries/ostree*conf").Output()
		if err != nil {
			return errors.Wrapf(err, "finding bootloader config path")
		}
		confs := strings.Split(strings.TrimSpace(string(confpathout)), "\n")
		if len(confs) != 1 {
			return fmt.Errorf("Multiple values for bootloader config: %v", confpathout)
		}
		confpath := confs[0]

		origconf, err := exec.Command("guestfish", gf.remote, "read-file", confpath).Output()
		if err != nil {
			return errors.Wrapf(err, "reading bootloader config")
		}
		var buf strings.Builder
		for _, line := range strings.Split(string(origconf), "\n") {
			if strings.HasPrefix(line, "options ") {
				line += " " + kargs
			}
			buf.Write([]byte(line))
			buf.Write([]byte("\n"))
		}
		if err := exec.Command("guestfish", gf.remote, "write", confpath, buf.String()).Run(); err != nil {
			return errors.Wrapf(err, "writing bootloader config")
		}
	}

	if err := exec.Command("guestfish", gf.remote, "umount-all").Run(); err != nil {
		return errors.Wrapf(err, "guestfish umount failed")
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

	imgOpts := []string{"create", "-f", disk.AttachFormat(), dstFileName}
	if disk.BackingFile != "" {
		backingFile, err := resolveBackingFile(disk.BackingFile)
		if err != nil {
			return err
		}
		imgOpts = append(imgOpts, "-o", fmt.Sprintf("backing_file=%s,lazy_refcounts=on", backingFile))
		if disk.AttachFormat() == "raw" {
			// TODO This copies the whole disk right now; we should figure out how to
			// either turn off locking (the `file` driver has a `locking=off` option,
			// might require a qemu patch to do for qcow2) or figure out if there's a different
			// way to do virtual multipath.
			imgOpts = []string{"convert", "-f", "qcow2", "-O", "raw", backingFile, dstFileName}
		}
	}

	if disk.Size != "" {
		imgOpts = append(imgOpts, disk.Size)
	}
	qemuImg := exec.Command("qemu-img", imgOpts...)
	qemuImg.Stderr = os.Stderr

	if err := qemuImg.Run(); err != nil {
		return err
	}

	if primary {
		// If the board doesn't support -fw_cfg or we were explicitly
		// requested, inject via libguestfs on the primary disk.
		requiresInjection := builder.ConfigFile != "" && (builder.ForceConfigInjection || !builder.supportsFwCfg())
		if requiresInjection || builder.IgnitionNetworkKargs != "" || builder.AppendKernelArguments != "" {
			if err = setupPreboot(builder.ConfigFile, builder.IgnitionNetworkKargs, builder.AppendKernelArguments,
				dstFileName, disk.SectorSize); err != nil {
				return errors.Wrapf(err, "ignition injection with guestfs failed")
			}
			builder.configInjected = true
		}
	}
	fd, err := os.OpenFile(dstFileName, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	diskOpts := disk.DeviceOpts
	if primary {
		diskOpts = append(diskOpts, "serial=primary-disk")
	} else {
		foundserial := false
		for _, opt := range diskOpts {
			if strings.HasPrefix(opt, "serial=") {
				foundserial = true
			}
		}
		if !foundserial {
			// Note that diskId is incremented by addDiskFd
			diskOpts = append(diskOpts, "serial="+fmt.Sprintf("disk%d", builder.diskId))
		}
	}
	channel := disk.Channel
	if channel == "" {
		channel = "virtio"
	}
	if disk.SectorSize != 0 {
		diskOpts = append(diskOpts, fmt.Sprintf("physical_block_size=%[1]d,logical_block_size=%[1]d", disk.SectorSize))
	}
	// Primary disk gets bootindex 1, all other disks have unspecified
	// bootindex, which means lower priority.
	if primary {
		diskOpts = append(diskOpts, "bootindex=1")
	}
	builder.addDiskFd(fd, channel, disk, diskOpts)
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

// AddInstallIso adds an ISO image
func (builder *QemuBuilder) AddInstallIso(path string) error {
	// We use bootindex=2 here: the idea is that during an ISO install, the
	// primary disk isn't bootable, so the bootloader will fall back to the ISO
	// boot. On reboot when the system is installed, the primary disk is
	// selected. This allows us to have "boot once" functionality on both UEFI
	// and BIOS (`-boot once=d` OTOH doesn't work with OVMF).
	builder.Append("-drive", "file="+path+",format=raw,if=none,readonly=on,id=installiso", "-device", "ide-cd,drive=installiso,bootindex=2")
	return nil
}

func (builder *QemuBuilder) finalize() {
	if builder.finalized {
		return
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
		switch system.RpmArch() {
		case "aarch64":
		case "s390x":
		case "ppc64le":
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
func baseQemuArgs() []string {
	switch system.RpmArch() {
	case "x86_64":
		return []string{
			"qemu-system-x86_64",
			"-machine", "accel=kvm",
			"-cpu", "host",
		}
	case "aarch64":
		return []string{
			"qemu-system-aarch64",
			"-machine", "virt,accel=kvm,gic-version=max",
			"-cpu", "host",
		}
	case "s390x":
		return []string{
			"qemu-system-s390x",
			"-machine", "s390-ccw-virtio,accel=kvm",
			"-cpu", "host",
		}
	case "ppc64le":
		return []string{
			"qemu-system-ppc64",
			"-machine", "pseries,accel=kvm,kvm-type=HV",
		}
	default:
		panic(fmt.Sprintf("RpmArch %s combo not supported for qemu ", system.RpmArch()))
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

	argv := baseQemuArgs()
	argv = append(argv, "-m", fmt.Sprintf("%d", builder.Memory))

	if builder.Processors < 0 {
		nproc, err := system.GetProcessors()
		if err != nil {
			return nil, errors.Wrapf(err, "qemu estimating processors")
		}
		// cap qemu smp at some reasonable level; sometimes our tooling runs
		// on 32-core servers (64 hyperthreads) and there's no reason to
		// try to match that.
		nproc = uint(math.Max(float64(nproc), float64(16)))
		builder.Processors = int(nproc)
	} else if builder.Processors == 0 {
		builder.Processors = 1
	}
	argv = append(argv, "-smp", fmt.Sprintf("%d", builder.Processors))

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
		"-device", virtio("rng", "rng=rng0"))
	if builder.Uuid != "" {
		argv = append(argv, "-uuid", builder.Uuid)
	}

	// We never want a popup window
	argv = append(argv, "-nographic")

	// Handle Ignition
	if builder.ConfigFile != "" && !builder.configInjected {
		if builder.supportsFwCfg() {
			builder.Append("-fw_cfg", "name=opt/com.coreos/config,file="+builder.ConfigFile)
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

	if builder.cleanupConfig {
		inst.tmpConfig = builder.ConfigFile
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
