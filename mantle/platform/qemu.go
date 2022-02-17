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

// qemu.go is a Go interface to running `qemu` as a subprocess.
//
// Why not libvirt?
// Two main reasons.  First, we really do want to use qemu, and not
// something else.  We rely on qemu features/APIs and there's a general
// assumption that the qemu process is local (e.g. we expose 9p filesystem
// sharing).  Second, libvirt runs as a daemon, but we want the
// VMs "lifecycle bound" to their creating process (e.g. kola),
// so that e.g. Ctrl-C (SIGINT) kills both reliably.
//
// Other related projects (as a reference to share ideas if not code)
// https://github.com/google/syzkaller/blob/3e84253bf41d63c55f92679b1aab9102f2f4949a/vm/qemu/qemu.go
// https://github.com/intel/govmm

package platform

import (
	"bufio"
	"bytes"
	"context"
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
	"github.com/digitalocean/go-qemu/qmp"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/pkg/errors"
)

var (
	// ErrInitramfsEmergency is the marker error returned upon node blocking in emergency mode in initramfs.
	ErrInitramfsEmergency = errors.New("entered emergency.target in initramfs")
)

// HostForwardPort contains details about port-forwarding for the VM.
type HostForwardPort struct {
	Service   string
	HostPort  int
	GuestPort int
}

// QemuMachineOptions is specialized MachineOption struct for QEMU.
type QemuMachineOptions struct {
	MachineOptions
	HostForwardPorts    []HostForwardPort
	DisablePDeathSig    bool
	OverrideBackingFile string
}

// QEMUMachine represents a qemu instance.
type QEMUMachine interface {
	// Embedding the Machine interface
	Machine

	// RemovePrimaryBlockDevice removes the primary device from a given qemu
	// instance and sets the secondary device as primary.
	RemovePrimaryBlockDevice() error
}

// Disk holds the details of a virtual disk.
type Disk struct {
	Size          string   // disk image size in bytes, optional suffixes "K", "M", "G", "T" allowed.
	BackingFile   string   // raw disk image to use.
	BackingFormat string   // qcow2, raw, etc.  If unspecified will be autodetected.
	Channel       string   // virtio (default), nvme
	DeviceOpts    []string // extra options to pass to qemu. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
	SectorSize    int      // if not 0, override disk sector size
	NbdDisk       bool     // if true, the disks should be presented over nbd:unix socket
	MultiPathDisk bool     // if true, present multiple paths

	attachEndPoint string   // qemuPath to attach to
	dstFileName    string   // the prepared file
	nbdServCmd     exec.Cmd // command to serve the disk
}

// ParseDiskSpec converts a disk specification into a Disk. The format is:
// <size>[:<opt1>,<opt2>,...].
func ParseDiskSpec(spec string) (*Disk, error) {
	split := strings.Split(spec, ":")
	var size string
	multipathed := false
	if len(split) == 1 {
		size = split[0]
	} else if len(split) == 2 {
		size = split[0]
		for _, opt := range strings.Split(split[1], ",") {
			if opt == "mpath" {
				multipathed = true
			} else {
				return nil, fmt.Errorf("unknown disk option %s", opt)
			}
		}
	} else {
		return nil, fmt.Errorf("invalid disk spec %s", spec)
	}
	return &Disk{
		Size:          size,
		MultiPathDisk: multipathed,
	}, nil
}

// bootIso is an internal struct used by AddIso() and setupIso()
type bootIso struct {
	path      string
	bootindex string
}

// QemuInstance holds an instantiated VM through its lifecycle.
type QemuInstance struct {
	qemu               exec.Cmd
	tempdir            string
	swtpm              exec.Cmd
	nbdServers         []exec.Cmd
	hostForwardedPorts []HostForwardPort

	journalPipe *os.File

	qmpSocket     *qmp.SocketMonitor
	qmpSocketPath string
}

// Pid returns the PID of QEMU process.
func (inst *QemuInstance) Pid() int {
	return inst.qemu.Pid()
}

// Kill kills the VM instance.
func (inst *QemuInstance) Kill() error {
	plog.Debugf("Killing qemu (%v)", inst.qemu.Pid())
	return inst.qemu.Kill()
}

// SSHAddress returns the IP address with the forwarded port (host-side).
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
func (inst *QemuInstance) WaitIgnitionError(ctx context.Context) (string, error) {
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
		if err := ctx.Err(); err != nil {
			return "", err
		}

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
func (inst *QemuInstance) WaitAll(ctx context.Context) error {
	c := make(chan error)
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Early stop due to failure in initramfs.
	go func() {
		buf, err := inst.WaitIgnitionError(waitCtx)
		if err != nil {
			c <- err
			return
		}

		// TODO: parse buf and try to nicely render something.
		if buf != "" {
			c <- ErrInitramfsEmergency
			return
		}
	}()

	// Machine terminated.
	go func() {
		select {
		case <-waitCtx.Done():
			c <- waitCtx.Err()
		case c <- inst.Wait():
		}

	}()

	return <-c
}

// Destroy kills the instance and associated sidecar processes.
func (inst *QemuInstance) Destroy() {
	if inst.qmpSocket != nil {
		inst.qmpSocket.Disconnect() //nolint // Ignore Errors
		inst.qmpSocket = nil
		os.Remove(inst.qmpSocketPath) //nolint // Ignore Errors
	}
	if inst.journalPipe != nil {
		inst.journalPipe.Close()
		inst.journalPipe = nil
	}
	// kill is safe if already dead
	if err := inst.Kill(); err != nil {
		plog.Errorf("Error killing qemu instance %v: %v", inst.Pid(), err)
	}
	if inst.swtpm != nil {
		inst.swtpm.Kill() //nolint // Ignore errors
		inst.swtpm = nil
	}
	for _, nbdServ := range inst.nbdServers {
		if nbdServ != nil {
			nbdServ.Kill() //nolint // Ignore errors
		}
	}
	inst.nbdServers = nil

	if inst.tempdir != "" {
		if err := os.RemoveAll(inst.tempdir); err != nil {
			plog.Errorf("Error removing tempdir: %v", err)
		}
	}
}

// SwitchBootOrder tweaks the boot order for the instance.
// Currently effective on aarch64: switches the boot order to boot from disk on reboot. For s390x and aarch64, bootindex
// is used to boot from the network device (boot once is not supported). For s390x, the boot ordering was not a problem as it
// would always read from disk first. For aarch64, the bootindex needs to be switched to boot from disk before a reboot
func (inst *QemuInstance) SwitchBootOrder() (err2 error) {
	if system.RpmArch() != "s390x" && system.RpmArch() != "aarch64" {
		//Not applicable for other arches
		return nil
	}
	devs, err := inst.listDevices()
	if err != nil {
		return errors.Wrapf(err, "Could not list devices through qmp")
	}
	blkdevs, err := inst.listBlkDevices()
	if err != nil {
		return errors.Wrapf(err, "Could not list blk devices through qmp")
	}

	var bootdev, primarydev, secondarydev string
	// Get bootdevice for pxe boot
	for _, dev := range devs.Return {
		switch dev.Type {
		case "child<virtio-net-pci>", "child<virtio-net-ccw>":
			bootdev = filepath.Join("/machine/peripheral-anon", dev.Name)
		default:
			break
		}
	}
	// Get boot device (for iso-installs) and block device
	for _, dev := range blkdevs.Return {
		devpath := filepath.Clean(strings.Trim(dev.DevicePath, "virtio-backend"))
		switch dev.Device {
		case "installiso":
			bootdev = devpath
		case "d1", "mpath10":
			primarydev = devpath
		case "mpath11":
			secondarydev = devpath
		default:
			break
		}
	}

	// unset bootindex for the boot device
	if err := inst.setBootIndexForDevice(bootdev, -1); err != nil {
		return errors.Wrapf(err, "Could not set bootindex for bootdev")
	}
	// set bootindex to 1 to boot from disk
	if err := inst.setBootIndexForDevice(primarydev, 1); err != nil {
		return errors.Wrapf(err, "Could not set bootindex for primarydev")
	}
	// set bootindex to 2 for secondary multipath disk
	if secondarydev != "" {
		if err := inst.setBootIndexForDevice(secondarydev, 2); err != nil {
			return errors.Wrapf(err, "Could not set bootindex for secondarydev")
		}
	}
	return nil
}

// RemovePrimaryBlockDevice deletes the primary device from a qemu instance
// and sets the seconday device as primary. It expects that all block devices
// are mirrors.
func (inst *QemuInstance) RemovePrimaryBlockDevice() (err2 error) {
	var primaryDevice string
	var secondaryDevicePath string

	blkdevs, err := inst.listBlkDevices()
	if err != nil {
		return errors.Wrapf(err, "Could not list block devices through qmp")
	}
	// This tries to identify the primary device by looking into
	// a `BackingFileDepth` parameter of a device and check if
	// it is a removable and part of `virtio-blk-pci` devices.
	for _, dev := range blkdevs.Return {
		if !dev.Removable && strings.Contains(dev.DevicePath, "virtio-backend") {
			if dev.Inserted.BackingFileDepth == 1 {
				primaryDevice = dev.DevicePath
			} else {
				secondaryDevicePath = dev.DevicePath
			}
		}
	}
	if err := inst.setBootIndexForDevice(primaryDevice, -1); err != nil {
		return errors.Wrapf(err, "Could not set bootindex for %v", primaryDevice)
	}
	primaryDevice = primaryDevice[:strings.LastIndex(primaryDevice, "/")]
	if err := inst.deleteBlockDevice(primaryDevice); err != nil {
		return errors.Wrapf(err, "Could not delete primary device %v", primaryDevice)
	}
	if len(secondaryDevicePath) == 0 {
		return errors.Wrapf(err, "Could not find secondary device")
	}
	if err := inst.setBootIndexForDevice(secondaryDevicePath, 1); err != nil {
		return errors.Wrapf(err, "Could not set bootindex for  %v", secondaryDevicePath)
	}

	return nil
}

// QemuBuilder is a configurator that can then create a qemu instance
type QemuBuilder struct {
	// ConfigFile is a path to Ignition configuration
	ConfigFile string
	// ForceConfigInjection is useful for booting `metal` images directly
	ForceConfigInjection bool
	configInjected       bool

	// File to which to redirect the serial console
	ConsoleFile string

	// Memory defaults to 1024 on most architectures, others it may be 2048
	Memory int
	// Processors < 0 means to use host count, unset means 1, values > 1 are directly used
	Processors int
	UUID       string
	Firmware   string
	Swtpm      bool
	Pdeathsig  bool
	Argv       []string

	// AppendKernelArgs are appended to the bootloader config
	AppendKernelArgs string

	// AppendFirstbootKernelArgs are written to /boot/ignition
	AppendFirstbootKernelArgs string

	Hostname string

	InheritConsole bool

	iso         *bootIso
	isoAsDisk   bool
	primaryDisk *Disk
	// primaryIsBoot is true if the only boot media should be the primary disk
	primaryIsBoot bool

	// tempdir holds our temporary files
	tempdir string

	// ignition is a config object that can be used instead of
	// ConfigFile.
	ignition         *conf.Conf
	ignitionSet      bool
	ignitionRendered bool

	UsermodeNetworking        bool
	RestrictNetworking        bool
	requestedHostForwardPorts []HostForwardPort
	additionalNics            int

	finalized bool
	diskID    uint
	disks     []*Disk
	fs9pID    uint
	// virtioSerialID is incremented for each device
	virtioSerialID uint
	// fds is file descriptors we own to pass to qemu
	fds []*os.File
}

// NewQemuBuilder creates a new build for QEMU with default settings.
func NewQemuBuilder() *QemuBuilder {
	ret := QemuBuilder{
		Firmware:  "bios",
		Swtpm:     true,
		Pdeathsig: true,
		Argv:      []string{},
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
func (builder *QemuBuilder) SetConfig(config *conf.Conf) {
	if builder.ignitionRendered {
		panic("SetConfig called after config rendered")
	}
	if builder.ignitionSet {
		panic("SetConfig called multiple times")
	}
	builder.ignition = config
	builder.ignitionSet = true
}

// Small wrapper around ioutil.Tempfile() to avoid leaking our tempdir to
// others.
func (builder *QemuBuilder) TempFile(pattern string) (*os.File, error) {
	if err := builder.ensureTempdir(); err != nil {
		return nil, err
	}
	return ioutil.TempFile(builder.tempdir, pattern)
}

// renderIgnition lazily renders a parsed config if one is set
func (builder *QemuBuilder) renderIgnition() error {
	if !builder.ignitionSet || builder.ignitionRendered {
		return nil
	}
	if builder.ConfigFile != "" {
		panic("Both ConfigFile and ignition set")
	}

	if err := builder.ensureTempdir(); err != nil {
		return err
	}
	builder.ConfigFile = filepath.Join(builder.tempdir, "config.ign")
	if err := builder.ignition.WriteFile(builder.ConfigFile); err != nil {
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
	case "x86_64", "ppc64le", "aarch64":
		suffix = "pci"
	case "s390x":
		suffix = "ccw"
	default:
		panic(fmt.Sprintf("RpmArch %s unhandled in virtio()", system.RpmArch()))
	}
	return fmt.Sprintf("virtio-%s-%s,%s", device, suffix, args)
}

// EnableUsermodeNetworking configure forwarding for all requested ports,
// via usermode network helpers.
func (builder *QemuBuilder) EnableUsermodeNetworking(h []HostForwardPort) {
	builder.UsermodeNetworking = true
	builder.requestedHostForwardPorts = h
}

func (builder *QemuBuilder) AddAdditionalNics(additionalNics int) {
	builder.additionalNics = additionalNics
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

func (builder *QemuBuilder) setupAdditionalNetworking() error {
	macCounter := 0
	netOffset := 30
	for i := 1; i <= builder.additionalNics; i++ {
		idSuffix := fmt.Sprintf("%d", i)
		netSuffix := fmt.Sprintf("%d", netOffset+i)
		macSuffix := fmt.Sprintf("%02x", macCounter)

		netdev := fmt.Sprintf("user,id=eth%s,dhcpstart=10.0.2.%s", idSuffix, netSuffix)
		device := virtio("net", fmt.Sprintf("netdev=eth%s,mac=52:55:00:d1:56:%s", idSuffix, macSuffix))
		builder.Append("-netdev", netdev, "-device", device)
		macCounter++
	}

	return nil
}

// Mount9p sets up a mount point from the host to guest.  To be replaced
// with https://virtio-fs.gitlab.io/ once it lands everywhere.
func (builder *QemuBuilder) Mount9p(source, destHint string, readonly bool) {
	builder.fs9pID++
	readonlyStr := ""
	if readonly {
		readonlyStr = ",readonly=on"
	}
	builder.Append("--fsdev", fmt.Sprintf("local,id=fs%d,path=%s,security_model=mapped%s", builder.fs9pID, source, readonlyStr))
	builder.Append("-device", virtio("9p", fmt.Sprintf("fsdev=fs%d,mount_tag=%s", builder.fs9pID, destHint)))
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
	if system.RpmArch() == "s390x" {
		// ppc64le and aarch64 support TPM as of f33. s390x does not support a backend for TPM
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
	guestfishArgs := []string{"--listen"}
	if diskSectorSize != 0 {
		guestfishArgs = append(guestfishArgs, fmt.Sprintf("--blocksize=%d", diskSectorSize))
	}
	guestfishArgs = append(guestfishArgs, "-a", diskImagePath)
	cmd := exec.Command("guestfish", guestfishArgs...)
	cmd.Env = append(os.Environ(), "LIBGUESTFS_BACKEND=direct")
	switch system.RpmArch() {
	case "ppc64le":
		cmd.Env = append(os.Environ(), "LIBGUESTFS_HV=/usr/lib/coreos-assembler/libguestfs-ppc64le-wrapper.sh")
	}
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
func setupPreboot(confPath, firstbootkargs, kargs string, diskImagePath string, diskSectorSize int) error {
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
	if firstbootkargs != "" {
		grubStr := fmt.Sprintf("set ignition_network_kcmdline='%s'\n", firstbootkargs)
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
		format := disk.BackingFormat
		if format == "" {
			// QEMU 5 warns if format is omitted, let's do detection for the common case
			// on our own.
			if strings.HasSuffix(backingFile, "qcow2") {
				format = "qcow2"
			}
		}
		if format != "" {
			qcow2Opts += fmt.Sprintf(",backing_fmt=%s", format)
		}
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

	builder.diskID++
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
		if requiresInjection || builder.AppendFirstbootKernelArgs != "" || builder.AppendKernelArgs != "" {
			if err := setupPreboot(builder.ConfigFile, builder.AppendFirstbootKernelArgs, builder.AppendKernelArgs,
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
			diskOpts = append(diskOpts, "serial="+fmt.Sprintf("disk%d", builder.diskID))
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

	id := fmt.Sprintf("d%d", builder.diskID)

	// Avoid file locking detection, and the disks we create
	// here are always currently ephemeral.
	defaultDiskOpts := "auto-read-only=off,cache=unsafe"

	if disk.MultiPathDisk {
		// Fake a NVME device with a fake WWN. All these attributes are needed in order
		// to trick multipath-tools that this is a "real" multipath device.
		// Each disk is presented on its own controller.

		// The WWN needs to be a unique uint64 number
		rand.Seed(time.Now().UnixNano())
		wwn := rand.Uint64()

		var bus string
		switch system.RpmArch() {
		case "x86_64", "ppc64le", "aarch64":
			bus = "pci"
		case "s390x":
			bus = "ccw"
		default:
			panic(fmt.Sprintf("Mantle doesn't know which bus type to use on %s", system.RpmArch()))
		}

		for i := 0; i < 2; i++ {
			if i == 1 {
				opts = strings.Replace(opts, "bootindex=1", "bootindex=2", -1)
			}
			pID := fmt.Sprintf("mpath%d%d", builder.diskID, i)
			scsiID := fmt.Sprintf("scsi_%s", pID)
			builder.Append("-device", fmt.Sprintf("virtio-scsi-%s,id=%s", bus, scsiID))
			builder.Append("-device",
				fmt.Sprintf("scsi-hd,bus=%s.0,drive=%s,vendor=NVME,product=VirtualMultipath,wwn=%d%s",
					scsiID, pID, wwn, opts))
			builder.Append("-drive", fmt.Sprintf("if=none,id=%s,format=raw,file=%s,media=disk,%s",
				pID, disk.attachEndPoint, defaultDiskOpts))
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

		// Default to cache=unsafe
		builder.Append("-drive", fmt.Sprintf("if=none,id=%s,file=%s,%s",
			id, disk.attachEndPoint, defaultDiskOpts))
	}
	return nil
}

// AddPrimaryDisk sets up the primary disk for the instance.
func (builder *QemuBuilder) AddPrimaryDisk(disk *Disk) error {
	if builder.primaryDisk != nil {
		return errors.New("Multiple primary disks specified")
	}
	// We do this one lazily in order to break an ordering requirement
	// for SetConfig() and AddPrimaryDisk() in the case where the
	// config needs to be injected into the disk.
	builder.primaryDisk = disk
	return nil
}

// AddBootDisk sets the instance to boot only from the target disk
func (builder *QemuBuilder) AddBootDisk(disk *Disk) error {
	if err := builder.AddPrimaryDisk(disk); err != nil {
		return err
	}
	builder.primaryIsBoot = true
	return nil
}

// AddDisk adds a secondary disk for the instance.
func (builder *QemuBuilder) AddDisk(disk *Disk) error {
	return builder.addDiskImpl(disk, false)
}

// AddDisksFromSpecs adds multiple secondary disks from their specs.
func (builder *QemuBuilder) AddDisksFromSpecs(specs []string) error {
	for _, spec := range specs {
		if disk, err := ParseDiskSpec(spec); err != nil {
			return errors.Wrapf(err, "parsing additional disk spec '%s'", spec)
		} else if err = builder.AddDisk(disk); err != nil {
			return errors.Wrapf(err, "adding additional disk '%s'", spec)
		}
	}
	return nil
}

// AddIso adds an ISO image, optionally configuring its boot index
// If asDisk is set, attach the ISO as a disk drive (as though it was copied
// to a USB stick) and overwrite the El Torito signature in the image
// (to force QEMU's UEFI firmware to boot via the hybrid ESP).
func (builder *QemuBuilder) AddIso(path string, bootindexStr string, asDisk bool) error {
	builder.iso = &bootIso{
		path:      path,
		bootindex: bootindexStr,
	}
	builder.isoAsDisk = asDisk
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

// Append appends additional arguments for QEMU.
func (builder *QemuBuilder) Append(args ...string) {
	builder.Argv = append(builder.Argv, args...)
}

// baseQemuArgs takes a board and returns the basic qemu
// arguments needed for the current architecture.
func baseQemuArgs() []string {
	accel := "accel=kvm"
	kvm := true
	if _, ok := os.LookupEnv("COSA_NO_KVM"); ok {
		accel = "accel=tcg"
		kvm = false
	}
	var ret []string
	switch system.RpmArch() {
	case "x86_64":
		ret = []string{
			"qemu-system-x86_64",
			"-machine", accel,
		}
	case "aarch64":
		ret = []string{
			"qemu-system-aarch64",
			"-machine", "virt,gic-version=max," + accel,
		}
	case "s390x":
		ret = []string{
			"qemu-system-s390x",
			"-machine", "s390-ccw-virtio," + accel,
		}
	case "ppc64le":
		ret = []string{
			"qemu-system-ppc64",
			"-machine", "pseries,kvm-type=HV,vsmt=8,cap-fwnmi=off," + accel,
		}
	default:
		panic(fmt.Sprintf("RpmArch %s combo not supported for qemu ", system.RpmArch()))
	}
	if kvm {
		ret = append(ret, "-cpu", "host")
	}
	return ret
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
			return fmt.Errorf("architecture %s doesn't have support for secure boot in kola", system.RpmArch())
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
		builder.Append("-drive", "file=/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw,if=pflash,format=raw,unit=0,readonly=on,auto-read-only=off")
		builder.Append("-drive", fmt.Sprintf("file=%s,if=pflash,format=raw,unit=1,readonly=off,auto-read-only=off", fdset))
	default:
		panic(fmt.Sprintf("Architecture %s doesn't have support for UEFI in qemu.", system.RpmArch()))
	}

	return nil
}

// Checks whether coreos-installer has
// https://github.com/coreos/coreos-installer/pull/341. Can be dropped once
// that PR is in all the cosa branches we care about.
func coreosInstallerSupportsISOKargs() (bool, error) {
	cmd := exec.Command("coreos-installer", "iso", "--help")
	cmd.Stderr = os.Stderr
	var outb bytes.Buffer
	cmd.Stdout = &outb
	if err := cmd.Run(); err != nil {
		return false, errors.Wrapf(err, "running coreos-installer iso --help")
	}
	out := outb.String()
	return strings.Contains(out, "kargs"), nil
}

func (builder *QemuBuilder) setupIso() error {
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
	if builder.ConfigFile != "" {
		if builder.configInjected {
			panic("config already injected?")
		}
		configf, err := os.Open(builder.ConfigFile)
		if err != nil {
			return err
		}
		instCmd := exec.Command("coreos-installer", "iso", "ignition", "embed", isoEmbeddedPath)
		instCmd.Stdin = configf
		instCmd.Stderr = os.Stderr
		if err := instCmd.Run(); err != nil {
			return errors.Wrapf(err, "running coreos-installer iso embed")
		}
		builder.configInjected = true
	}

	if kargsSupported, err := coreosInstallerSupportsISOKargs(); err != nil {
		return err
	} else if kargsSupported {
		allargs := fmt.Sprintf("console=%s %s", consoleKernelArgument[system.RpmArch()], builder.AppendKernelArgs)
		instCmdKargs := exec.Command("coreos-installer", "iso", "kargs", "modify", "--append", allargs, isoEmbeddedPath)
		var stderrb bytes.Buffer
		instCmdKargs.Stderr = &stderrb
		if err := instCmdKargs.Run(); err != nil {
			// Don't make this a hard error if it's just for console; we
			// may be operating on an old live ISO
			if len(builder.AppendKernelArgs) > 0 {
				return errors.Wrapf(err, "running `coreos-installer iso kargs modify`; old CoreOS ISO?")
			}
			stderr := stderrb.String()
			plog.Warningf("running coreos-installer iso kargs modify: %v: %q", err, stderr)
			plog.Warning("likely targeting an old CoreOS ISO; ignoring...")
		}
	} else if len(builder.AppendKernelArgs) > 0 {
		return fmt.Errorf("coreos-installer does not support appending kernel args")
	}

	if builder.isoAsDisk {
		f, err := os.OpenFile(isoEmbeddedPath, os.O_WRONLY, 0)
		if err != nil {
			return errors.Wrapf(err, "opening ISO image for writing")
		}
		defer f.Close()
		// Invalidate Boot System Identifier in the El Torito Boot
		// Record Volume Descriptor so the system must boot via the
		// MBR or ESP.  If we don't do this, QEMU's UEFI firmware
		// will boot via El Torito anyway, which doesn't match what
		// a lot of UEFI firmware does.
		_, err = f.WriteAt([]byte("NO"), 34823)
		if err != nil {
			return errors.Wrapf(err, "overwriting El Torito signature")
		}
	}
	builder.iso.path = isoEmbeddedPath

	// Arches s390x and ppc64le don't support UEFI and use the cdrom option to boot the ISO.
	// For all other arches we use ide-cd device with bootindex=2 here: the idea is
	// that during an ISO install, the primary disk isn't bootable, so the bootloader
	// will fall back to the ISO boot. On reboot when the system is installed, the
	// primary disk is selected. This allows us to have "boot once" functionality on
	// both UEFI and BIOS (`-boot once=d` OTOH doesn't work with OVMF).
	switch system.RpmArch() {
	case "s390x", "ppc64le", "aarch64":
		if builder.isoAsDisk {
			// we could do it, but boot would fail
			return errors.New("cannot attach ISO as disk; no hybrid ISO on this arch")
		}
		builder.Append("-drive", "file="+builder.iso.path+",id=installiso,index=2,media=cdrom")
	default:
		bootindexStr := ""
		if builder.iso.bootindex != "" {
			bootindexStr = "," + builder.iso.bootindex
		}
		builder.Append("-drive", "file="+builder.iso.path+",format=raw,if=none,readonly=on,id=installiso")
		if builder.isoAsDisk {
			builder.Append("-device", virtio("blk", "drive=installiso"+bootindexStr))
		} else {
			builder.Append("-device", "ide-cd,drive=installiso"+bootindexStr)
		}
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
	if builder.virtioSerialID == 0 {
		builder.Append("-device", "virtio-serial")
	}
	builder.virtioSerialID++
	id := fmt.Sprintf("virtioserial%d", builder.virtioSerialID)
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
// (post-switchroot) over a virtio-serial channel.
// - The first parameter is a poitner to the configuration of the target VM.
// - The second parameter is an optional queryArguments to filter the stream -
//   see `man journalctl` for more information.
// - The return value is a file stream which will be newline-separated JSON.
func (builder *QemuBuilder) VirtioJournal(config *conf.Conf, queryArguments string) (*os.File, error) {
	stream, err := builder.VirtioChannelRead("mantlejournal")
	if err != nil {
		return nil, err
	}
	var streamJournalUnit = fmt.Sprintf(`[Unit]
	Requires=dev-virtio\\x2dports-mantlejournal.device
	IgnoreOnIsolate=true
	[Service]
	Type=simple
	StandardOutput=file:/dev/virtio-ports/mantlejournal
	# Wrap in /bin/bash to hack around SELinux
	# https://bugzilla.redhat.com/show_bug.cgi?id=1942198
	ExecStart=/usr/bin/bash -c "journalctl -q -b -f -o json --no-tail %s"
	[Install]
	RequiredBy=basic.target
	`, queryArguments)

	config.AddSystemdUnit("mantle-virtio-journal-stream.service", streamJournalUnit, conf.Enable)

	return stream, nil
}

// Exec tries to run a QEMU instance with the given settings.
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
	if builder.UUID != "" {
		argv = append(argv, "-uuid", builder.UUID)
	}

	// We never want a popup window
	argv = append(argv, "-nographic")

	// We want to customize everything from scratch, so avoid defaults
	argv = append(argv, "-nodefaults")

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
		if builder.primaryIsBoot {
			argv = append(argv, "-boot", "order=c,strict=on")
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

	// Handle Additional NICs networking
	if builder.additionalNics > 0 {
		if err := builder.setupAdditionalNetworking(); err != nil {
			return nil, err
		}
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
		if err != nil {
			return nil, err
		}
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

	// Set up QMP (currently used to switch boot order after first boot on aarch64.
	// The qmp socket path must be unique to the instance.
	inst.qmpSocketPath = filepath.Join(builder.tempdir, fmt.Sprintf("qmp-%d.sock", time.Now().UnixNano()))
	qmpID := "qemu-qmp"
	builder.Append("-chardev", fmt.Sprintf("socket,id=%s,path=%s,server=on,wait=off", qmpID, inst.qmpSocketPath))
	builder.Append("-mon", fmt.Sprintf("chardev=%s,mode=control", qmpID))

	// Set up the virtio channel to get Ignition failures by default
	journalPipeR, err := builder.VirtioChannelRead("com.coreos.ignition.journal")
	inst.journalPipe = journalPipeR
	if err != nil {
		return nil, err
	}

	fdnum := 3 // first additional file starts at position 3
	for i := range builder.fds {
		fdset := i + 1 // Start at 1
		argv = append(argv, "-add-fd", fmt.Sprintf("fd=%d,set=%d", fdnum, fdset))
		fdnum++
	}

	if builder.ConsoleFile != "" {
		builder.Append("-display", "none", "-chardev", "file,id=log,path="+builder.ConsoleFile, "-serial", "chardev:log")
	} else {
		builder.Append("-serial", "mon:stdio")
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

	plog.Debugf("Started qemu (%v) with args: %v", inst.qemu.Pid(), argv)

	// Transfer ownership of the tempdir
	inst.tempdir = builder.tempdir
	builder.tempdir = ""
	cleanupInst = false

	// Connect to the QMP socket which allows us to control qemu.  We wait up to 30s
	// to avoid flakes on loaded CI systems.  But, probably rather than bumping this
	// any higher it'd be better to try to reduce parallelism.
	if err := util.Retry(30, 1*time.Second,
		func() error {
			sockMonitor, err := qmp.NewSocketMonitor("unix", inst.qmpSocketPath, 2*time.Second)
			if err != nil {
				return err
			}
			inst.qmpSocket = sockMonitor
			return nil
		}); err != nil {
		return nil, fmt.Errorf("failed to establish qmp connection: %w", err)
	}
	if err := inst.qmpSocket.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect over qmp to qemu instance")
	}

	return &inst, nil
}

// Close drops all resources owned by the builder.
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
