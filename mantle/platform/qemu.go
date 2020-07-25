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
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"

	v3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/pkg/errors"
)

var (
	ErrInitramfsEmergency = errors.New("entered emergency.target in initramfs")
)

type HostForwardPort struct {
	Service   string
	HostPort  int
	GuestPort int
}

type QemuMachineOptions struct {
	MachineOptions
	HostForwardPorts []HostForwardPort
	DisablePDeathSig bool
}

type Disk struct {
	Size          string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed. Incompatible with BackingFile
	BackingFile   string   // raw disk image to use. Incompatible with Size.
	Channel       string   // virtio (default), nvme
	DeviceOpts    []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
	SectorSize    int      // if not 0, override disk sector size
	NbdDisk       bool     // if true, the disks should be presented over nbd:unix socket
	MultiPathDisk bool     // if true, present multiple paths

	attachEndPoint string   // qemuPath to attach to
	fd             *os.File // builder file descriptor location, e.g. /proc/self/fd/
	dstFileName    string   // the prepared file
	nbdServCmd     exec.Cmd // command to serve the disk
}

// bootIso is an internal struct used by AddIso() and setupIso()
type bootIso struct {
	path      string
	bootindex string
}

type QemuInstance struct {
	qemu               exec.Cmd
	tmpConfig          string
	tempdir            string
	swtpm              exec.Cmd
	nbdServers         []exec.Cmd
	hostForwardedPorts []HostForwardPort

	journalPipe *os.File
}

func (inst *QemuInstance) Pid() int {
	return inst.qemu.Pid()
}

func (inst *QemuInstance) Kill() error {
	return inst.qemu.Kill()
}

// Get the IP address with the forwarded port
func (inst *QemuInstance) SSHAddress() (string, error) {
	for _, fwdPorts := range inst.hostForwardedPorts {
		if fwdPorts.Service == "ssh" {
			return fmt.Sprintf("127.0.0.1:%d", fwdPorts.HostPort), nil
		}
	}
	return "", fmt.Errorf("didn't find an address")
}

// Wait for the qemu process to exit
func (inst *QemuInstance) Wait() error {
	return inst.qemu.Wait()
}

// WaitIgnitionError will only return if the instance
// failed inside the initramfs.  The resulting string will
// be a newline-delimited stream of JSON strings, as returned
// by `journalctl -o json`.
func (inst *QemuInstance) WaitIgnitionError() (string, error) {
	b := bufio.NewReaderSize(inst.journalPipe, 64768)
	var r strings.Builder
	iscorrupted := false
	_, err := b.Peek(1)
	if err != nil {
		// It's normal to get EOF if we didn't catch an error and qemu
		// is shutting down.  We also need to handle when the Destroy()
		// function closes the journal FD on us.
		if e, ok := err.(*os.PathError); ok && e.Err == os.ErrClosed {
			return "", nil
		} else if err == io.EOF {
			return "", nil
		}
		return "", errors.Wrapf(err, "Reading from journal")
	}
	for {
		line, prefix, err := b.ReadLine()
		if err != nil {
			return r.String(), errors.Wrapf(err, "Reading from journal channel")
		}
		if prefix {
			iscorrupted = true
		}
		if len(line) == 0 || string(line) == "{}" {
			break
		}
		r.Write(line)
		r.Write([]byte("\n"))
	}
	if iscorrupted {
		return r.String(), fmt.Errorf("journal was truncated due to overly long line")
	}
	return r.String(), nil
}

// WaitAll wraps the process exit as well as WaitIgnitionError,
// returning an error if either fail.
func (inst *QemuInstance) WaitAll() error {
	c := make(chan error)
	go func() {
		buf, err := inst.WaitIgnitionError()
		if err != nil {
			c <- err
		} else {
			// TODO parse buf and try to nicely render something
			if buf != "" {
				c <- ErrInitramfsEmergency
			}
		}
	}()
	go func() {
		c <- inst.Wait()
	}()
	return <-c
}

func (inst *QemuInstance) Destroy() {
	if inst.journalPipe != nil {
		inst.journalPipe.Close()
		inst.journalPipe = nil
	}
	// kill is safe if already dead
	if err := inst.qemu.Kill(); err != nil {
		plog.Errorf("Error killing qemu instance %v: %v", inst.Pid(), err)
	}
	if inst.swtpm != nil {
		inst.swtpm.Kill() // Ignore errors
		inst.swtpm = nil
	}
	for _, nbdServ := range inst.nbdServers {
		if nbdServ != nil {
			nbdServ.Kill() // Ignore errors
		}
	}
	inst.nbdServers = nil

	if inst.tempdir != "" {
		if err := os.RemoveAll(inst.tempdir); err != nil {
			plog.Errorf("Error removing tempdir: %v", err)
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

	iso         *bootIso
	primaryDisk *Disk

	MultiPathDisk bool

	// tempdir holds our temporary files
	tempdir string

	// ignition is a config object that can be used instead of
	// ConfigFile.
	ignition *v3types.Config
	// ignitionSpec2 says to convert to Ignition spec 2
	ignitionSpec2    bool
	ignitionSet      bool
	ignitionRendered bool

	UsermodeNetworking        bool
	RestrictNetworking        bool
	requestedHostForwardPorts []HostForwardPort

	finalized bool
	diskId    uint
	disks     []*Disk
	fs9pId    uint
	// virtioSerialId is incremented for each device
	virtioSerialId uint
	// fds is file descriptors we own to pass to qemu
	fds []*os.File
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

func (builder *QemuBuilder) ensureTempdir() error {
	if builder.tempdir != "" {
		return nil
	}
	tempdir, err := ioutil.TempDir("/var/tmp", "mantle-qemu")
	if err != nil {
		return err
	}
	builder.tempdir = tempdir
	return nil
}

// SetConfig injects Ignition; this can be used in place of ConfigFile.
func (builder *QemuBuilder) SetConfig(config v3types.Config, convSpec2 bool) {
	if builder.ignitionRendered {
		panic("SetConfig called after config rendered")
	}
	if builder.ignitionSet {
		panic("SetConfig called multiple times")
	}
	builder.ignition = &config
	builder.ignitionSpec2 = convSpec2
	builder.ignitionSet = true
}

// renderIgnition lazily renders a parsed config if one is set
func (builder *QemuBuilder) renderIgnition() error {
	if !builder.ignitionSet || builder.ignitionRendered {
		return nil
	}
	if builder.ConfigFile != "" {
		panic("Both ConfigFile and ignition set")
	}
	buf, err := conf.SerializeAndMaybeConvert(*builder.ignition, builder.ignitionSpec2)
	if err != nil {
		return err
	}
	if err := builder.ensureTempdir(); err != nil {
		return err
	}
	builder.ConfigFile = filepath.Join(builder.tempdir, "config.ign")
	err = ioutil.WriteFile(builder.ConfigFile, buf, 0644)
	if err != nil {
		return err
	}
	builder.ignition = nil
	builder.ignitionRendered = true
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

func (builder *QemuBuilder) ConsoleToFile(path string) {
	builder.Append("-display", "none", "-chardev", "file,id=log,path="+path, "-serial", "chardev:log")
}

func (builder *QemuBuilder) EnableUsermodeNetworking(h []HostForwardPort) {
	builder.UsermodeNetworking = true
	builder.requestedHostForwardPorts = h
}

func (builder *QemuBuilder) setupNetworking() error {
	netdev := "user,id=eth0"
	for i := range builder.requestedHostForwardPorts {
		address := fmt.Sprintf(":%d", builder.requestedHostForwardPorts[i].HostPort)
		// Possible race condition between getting the port here and using it
		// with qemu -- trade off for simpler port management
		l, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		l.Close()
		builder.requestedHostForwardPorts[i].HostPort = l.Addr().(*net.TCPAddr).Port
		netdev += fmt.Sprintf(",hostfwd=tcp:127.0.0.1:%d-:%d",
			builder.requestedHostForwardPorts[i].HostPort,
			builder.requestedHostForwardPorts[i].GuestPort)
	}

	if builder.Hostname != "" {
		netdev += fmt.Sprintf(",hostname=%s", builder.Hostname)
	}
	if builder.RestrictNetworking {
		netdev += ",restrict=on"
	}

	builder.Append("-netdev", netdev, "-device", virtio("net", "netdev=eth0"))
	return nil
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
	switch system.RpmArch() {
	// add back ppc64le as f32/f31's qemu doesn't yet support tpm device emulation
	// can be removed when cosa is rebased on top of f33/qemu5.0 and also aarch64
	case "s390x", "ppc64le", "aarch64":
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

// prepare creates the target disk and sets all the runtime attributes
// for use by the QemuBuilder.
func (disk *Disk) prepare(builder *QemuBuilder) error {
	if err := builder.ensureTempdir(); err != nil {
		return err
	}
	tmpf, err := ioutil.TempFile(builder.tempdir, "disk")
	if err != nil {
		return err
	}
	disk.dstFileName = tmpf.Name()

	imgOpts := []string{"create", "-f", "qcow2", disk.dstFileName}
	// On filesystems like btrfs, qcow2 files can become much more fragmented
	// if copy-on-write is enabled.  We don't need that, our disks are ephemeral.
	// https://gitlab.gnome.org/GNOME/gnome-boxes/-/issues/88
	// https://btrfs.wiki.kernel.org/index.php/Gotchas#Fragmentation
	// https://www.redhat.com/archives/libvir-list/2014-July/msg00361.html
	qcow2Opts := "nocow=on"
	if disk.BackingFile != "" {
		backingFile, err := resolveBackingFile(disk.BackingFile)
		if err != nil {
			return err
		}
		qcow2Opts += fmt.Sprintf(",backing_file=%s,lazy_refcounts=on", backingFile)
	}
	imgOpts = append(imgOpts, "-o", qcow2Opts)

	if disk.Size != "" {
		imgOpts = append(imgOpts, disk.Size)
	}
	qemuImg := exec.Command("qemu-img", imgOpts...)
	qemuImg.Stderr = os.Stderr

	if err := qemuImg.Run(); err != nil {
		return err
	}

	fdSet := builder.AddFd(tmpf)
	disk.attachEndPoint = fdSet

	// MultiPathDisks must be NBD remote mounted
	if disk.MultiPathDisk || disk.NbdDisk {
		socketName := fmt.Sprintf("%s.socket", disk.dstFileName)
		shareCount := "1"
		if disk.MultiPathDisk {
			shareCount = "2"
		}
		disk.nbdServCmd = exec.Command("qemu-nbd",
			"--format", "qcow2",
			"--cache", "unsafe",
			"--discard", "unmap",
			"--socket", socketName,
			"--share", shareCount,
			disk.dstFileName)
		disk.attachEndPoint = fmt.Sprintf("nbd:unix:%s", socketName)
	}

	builder.diskId += 1
	builder.disks = append(builder.disks, disk)
	return nil
}

func (builder *QemuBuilder) addDiskImpl(disk *Disk, primary bool) error {
	if err := disk.prepare(builder); err != nil {
		return err
	}
	if primary {
		// If the board doesn't support -fw_cfg or we were explicitly
		// requested, inject via libguestfs on the primary disk.
		if err := builder.renderIgnition(); err != nil {
			return errors.Wrapf(err, "rendering ignition")
		}
		requiresInjection := builder.ConfigFile != "" && builder.ForceConfigInjection
		if requiresInjection || builder.IgnitionNetworkKargs != "" || builder.AppendKernelArguments != "" {
			if err := setupPreboot(builder.ConfigFile, builder.IgnitionNetworkKargs, builder.AppendKernelArguments,
				disk.dstFileName, disk.SectorSize); err != nil {
				return errors.Wrapf(err, "ignition injection with guestfs failed")
			}
			builder.configInjected = true
		}
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

	opts := ""
	if len(diskOpts) > 0 {
		opts = "," + strings.Join(diskOpts, ",")
	}

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
			builder.Append("-drive", fmt.Sprintf("if=none,id=%s,file=%s,auto-read-only=off,media=disk",
				pId, disk.attachEndPoint))
		}
	} else {
		if !disk.NbdDisk {
			// In the non-multipath/nbd case we can just unlink the disk now
			// and avoid leaking space if we get Ctrl-C'd (though it's best if
			// higher level code catches SIGINT and cleans up the directory)
			os.Remove(disk.dstFileName)
		}
		disk.dstFileName = ""
		switch channel {
		case "virtio":
			builder.Append("-device", virtio("blk", fmt.Sprintf("drive=%s%s", id, opts)))
		case "nvme":
			builder.Append("-device", fmt.Sprintf("nvme,drive=%s%s", id, opts))
		default:
			panic(fmt.Sprintf("Unhandled channel: %s", channel))
		}

		builder.Append("-drive", fmt.Sprintf("if=none,id=%s,file=%s,auto-read-only=off",
			id, disk.attachEndPoint))
	}
	return nil
}

func (builder *QemuBuilder) AddPrimaryDisk(disk *Disk) error {
	if builder.primaryDisk != nil {
		panic("Multiple primary disks specified")
	}
	// We do this one lazily in order to break an ordering requirement
	// for SetConfig() and AddPrimaryDisk() in the case where the
	// config needs to be injected into the disk.
	builder.primaryDisk = disk
	return nil
}

func (builder *QemuBuilder) AddDisk(disk *Disk) error {
	return builder.addDiskImpl(disk, false)
}

// AddIso adds an ISO image, optionally configuring its boot index
func (builder *QemuBuilder) AddIso(path string, bootindexStr string) error {
	builder.iso = &bootIso{
		path:      path,
		bootindex: bootindexStr,
	}
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
		case "aarch64", "s390x", "ppc64le":
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
			"-machine", "pseries,accel=kvm,kvm-type=HV,vsmt=8",
		}
	default:
		panic(fmt.Sprintf("RpmArch %s combo not supported for qemu ", system.RpmArch()))
	}
}

func (builder *QemuBuilder) setupUefi(secureBoot bool) error {
	switch system.RpmArch() {
	case "x86_64":
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
	case "aarch64":
		if secureBoot {
			return fmt.Errorf("Architecture %s doesn't have support for secure boot in kola.", system.RpmArch())
		}
		vars, err := ioutil.TempFile("", "mantle-qemu")
		if err != nil {
			return err
		}
		//67108864 bytes is expected size of the "VARS" by qemu
		err = vars.Truncate(67108864)
		if err != nil {
			return err
		}

		_, err = vars.Seek(0, 0)
		if err != nil {
			return err
		}

		fdset := builder.AddFd(vars)
		builder.Append("-drive", fmt.Sprintf("file=/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw,if=pflash,format=raw,unit=0,readonly=on,auto-read-only=off"))
		builder.Append("-drive", fmt.Sprintf("file=%s,if=pflash,format=raw,unit=1,readonly=off,auto-read-only=off", fdset))
	default:
		panic(fmt.Sprintf("Architecture %s doesn't have support for UEFI in qemu.", system.RpmArch()))
	}

	return nil
}

func (builder *QemuBuilder) setupIso() error {
	if builder.ConfigFile != "" {
		if builder.configInjected {
			panic("config already injected?")
		}
		if err := builder.ensureTempdir(); err != nil {
			return err
		}
		// TODO change to use something like an unlinked tempfile (or O_TMPFILE)
		// in the same filesystem as the source so that reflinks (if available)
		// will work
		isoEmbeddedPath := filepath.Join(builder.tempdir, "install.iso")
		cpcmd := exec.Command("cp", "--reflink=auto", builder.iso.path, isoEmbeddedPath)
		cpcmd.Stderr = os.Stderr
		if err := cpcmd.Run(); err != nil {
			return errors.Wrapf(err, "copying iso")
		}
		configf, err := os.Open(builder.ConfigFile)
		if err != nil {
			return err
		}
		instCmd := exec.Command("coreos-installer", "iso", "embed", isoEmbeddedPath)
		instCmd.Stdin = configf
		instCmd.Stderr = os.Stderr
		if err := instCmd.Run(); err != nil {
			return errors.Wrapf(err, "running coreos-installer iso embed")
		}
		builder.iso.path = isoEmbeddedPath
		builder.configInjected = true
	}

	// Arches s390x and ppc64le don't support UEFI and use the cdrom option to boot the ISO.
	// For all other arches we use ide-cd device with bootindex=2 here: the idea is
	// that during an ISO install, the primary disk isn't bootable, so the bootloader
	// will fall back to the ISO boot. On reboot when the system is installed, the
	// primary disk is selected. This allows us to have "boot once" functionality on
	// both UEFI and BIOS (`-boot once=d` OTOH doesn't work with OVMF).
	switch system.RpmArch() {
	case "s390x", "ppc64le":
		builder.Append("-cdrom", builder.iso.path)
	case "aarch64":
		// TODO - can we boot from a virtual USB CDROM or a USB flash drive here?
		// https://fedoraproject.org/wiki/Architectures/AArch64/Install_with_QEMU#Installing_F23_aarch64_from_CDROM seems to claim yes
		return fmt.Errorf("Architecture aarch64 does not support ISO")
	default:
		bootindexStr := ""
		if builder.iso.bootindex != "" {
			bootindexStr = "," + builder.iso.bootindex
		}
		builder.Append("-drive", "file="+builder.iso.path+",format=raw,if=none,readonly=on,id=installiso", "-device", "ide-cd,drive=installiso"+bootindexStr)
	}

	return nil
}

// VirtioChannelRead allocates a virtio-serial channel that will appear in
// the guest as /dev/virtio-ports/<name>.  The guest can write to it, and
// the host can read.
func (builder *QemuBuilder) VirtioChannelRead(name string) (*os.File, error) {
	// Set up the virtio channel to get Ignition failures by default
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrapf(err, "virtioChannelRead creating pipe")
	}
	if builder.virtioSerialId == 0 {
		builder.Append("-device", "virtio-serial")
	}
	builder.virtioSerialId += 1
	id := fmt.Sprintf("virtioserial%d", builder.virtioSerialId)
	// https://www.redhat.com/archives/libvir-list/2015-December/msg00305.html
	builder.Append("-chardev", fmt.Sprintf("file,id=%s,path=%s,append=on", id, builder.AddFd(w)))
	builder.Append("-device", fmt.Sprintf("virtserialport,chardev=%s,name=%s", id, name))

	return r, nil
}

// SerialPipe reads the serial console output into a pipe
func (builder *QemuBuilder) SerialPipe() (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrapf(err, "virtioChannelRead creating pipe")
	}
	id := "serialpipe"
	builder.Append("-chardev", fmt.Sprintf("file,id=%s,path=%s,append=on", id, builder.AddFd(w)))
	builder.Append("-serial", fmt.Sprintf("chardev:%s", id))

	return r, nil
}

// VirtioJournal configures the OS and VM to stream the systemd journal
// (post-switchroot) over a virtio-serial channel.  The first return value
// is an Ignition fragment that should be included in the target config,
// and the file stream will be newline-separated JSON.  The optional
// queryArguments filters the stream - see `man journalctl` for more information.
func (builder *QemuBuilder) VirtioJournal(queryArguments string) (*v3types.Config, *os.File, error) {
	stream, err := builder.VirtioChannelRead("mantlejournal")
	if err != nil {
		return nil, nil, err
	}
	var streamJournalUnit = fmt.Sprintf(`[Unit]
	Requires=dev-virtio\\x2dports-mantlejournal.device
	IgnoreOnIsolate=true
	[Service]
	Type=simple
	StandardOutput=file:/dev/virtio-ports/mantlejournal
	ExecStart=/usr/bin/journalctl -q -b -f -o json --no-tail %s
	[Install]
	RequiredBy=multi-user.target
	`, queryArguments)

	conf := v3types.Config{
		Ignition: v3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: v3types.Systemd{
			Units: []v3types.Unit{
				{
					Name:     "mantle-virtio-journal-stream.service",
					Contents: &streamJournalUnit,
					Enabled:  util.BoolToPtr(true),
				},
			},
		},
	}
	return &conf, stream, nil
}

func (builder *QemuBuilder) Exec() (*QemuInstance, error) {
	builder.finalize()
	var err error

	if err := builder.renderIgnition(); err != nil {
		return nil, errors.Wrapf(err, "rendering ignition")
	}

	inst := QemuInstance{}
	cleanupInst := false
	defer func() {
		if cleanupInst {
			inst.Destroy()
		}
	}()

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
		if nproc > 16 {
			nproc = 16
		}

		builder.Processors = int(nproc)
	} else if builder.Processors == 0 {
		builder.Processors = 1
	}
	argv = append(argv, "-smp", fmt.Sprintf("%d", builder.Processors))

	switch builder.Firmware {
	case "bios":
		break
	case "uefi":
		if err := builder.setupUefi(false); err != nil {
			return nil, err
		}
	case "uefi-secure":
		if err := builder.setupUefi(true); err != nil {
			return nil, err
		}
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

	// We only render Ignition lazily, because we want to support calling
	// SetConfig() after AddPrimaryDisk() or AddInstallIso().
	if builder.iso != nil {
		if err := builder.setupIso(); err != nil {
			return nil, err
		}
	}
	if builder.primaryDisk != nil {
		if err := builder.addDiskImpl(builder.primaryDisk, true); err != nil {
			return nil, err
		}
	}

	// Handle Ignition if it wasn't already injected above
	if builder.ConfigFile != "" && !builder.configInjected {
		if builder.supportsFwCfg() {
			builder.Append("-fw_cfg", "name=opt/com.coreos/config,file="+builder.ConfigFile)
		} else {
			// Alternative to fw_cfg, should be generally usable on all arches,
			// especially those without fw_cfg support.
			// See https://github.com/coreos/ignition/pull/905
			builder.Append("-drive", fmt.Sprintf("if=none,id=ignition,format=raw,file=%s,readonly=on", builder.ConfigFile), "-device", "virtio-blk,serial=ignition,drive=ignition")
		}
	}

	// Start up the disks. Since the disk may be served via NBD,
	// we can't use builder.AddFd (no support for fdsets), so we at the disk to the tmpFiles.
	for _, disk := range builder.disks {
		if disk.nbdServCmd != nil {
			cmd := disk.nbdServCmd.(*exec.ExecCmd)
			if err := cmd.Start(); err != nil {
				return nil, errors.Wrapf(err, "spawing nbd server")
			}
			inst.nbdServers = append(inst.nbdServers, cmd)
		}
	}

	// Handle Usermode Networking
	if builder.UsermodeNetworking {
		if err := builder.setupNetworking(); err != nil {
			return nil, err
		}
		inst.hostForwardedPorts = builder.requestedHostForwardPorts
	}

	// Handle Software TPM
	if builder.Swtpm && builder.supportsSwtpm() {
		err = builder.ensureTempdir()
		if err != nil {
			return nil, err
		}
		swtpmSock := filepath.Join(builder.tempdir, "swtpm-sock")
		swtpmdir := filepath.Join(builder.tempdir, "swtpm")
		if err := os.Mkdir(swtpmdir, 0755); err != nil {
			return nil, err
		}

		inst.swtpm = exec.Command("swtpm", "socket", "--tpm2",
			"--ctrl", fmt.Sprintf("type=unixio,path=%s", swtpmSock),
			"--terminate", "--tpmstate", fmt.Sprintf("dir=%s", swtpmdir))
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
		// We need to wait until the swtpm starts up
		err = util.Retry(10, 500*time.Millisecond, func() error {
			_, err := os.Stat(swtpmSock)
			return err
		})
		argv = append(argv, "-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s", swtpmSock), "-tpmdev", "emulator,id=tpm0,chardev=chrtpm")
		// There are different device backends on each architecture
		switch system.RpmArch() {
		case "x86_64":
			argv = append(argv, "-device", "tpm-tis,tpmdev=tpm0")
		case "aarch64":
			argv = append(argv, "-device", "tpm-tis-device,tpmdev=tpm0")
		case "ppc64le":
			argv = append(argv, "-device", "tpm-spapr,tpmdev=tpm0")
		}

	}

	// Set up the virtio channel to get Ignition failures by default
	journalPipeR, err := builder.VirtioChannelRead("com.coreos.ignition.journal")
	inst.journalPipe = journalPipeR

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

	// Transfer ownership of the tempdir
	inst.tempdir = builder.tempdir
	builder.tempdir = ""
	cleanupInst = false

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

	if builder.tempdir != "" {
		os.RemoveAll(builder.tempdir)
	}
}
