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
// assumption that the qemu process is local (e.g. we expose 9p/virtiofs filesystem
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
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/digitalocean/go-qemu/qmp"

	"github.com/coreos/coreos-assembler/mantle/system"
	"github.com/coreos/coreos-assembler/mantle/system/exec"
	"github.com/pkg/errors"

	"golang.org/x/sys/unix"
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
	DeviceOpts    []string // extra options to pass to qemu -device. "serial=XXXX" makes disks show up as /dev/disk/by-id/virtio-<serial>
	DriveOpts     []string // extra options to pass to -drive
	SectorSize    int      // if not 0, override disk sector size
	NbdDisk       bool     // if true, the disks should be presented over nbd:unix socket
	MultiPathDisk bool     // if true, present multiple paths

	attachEndPoint string   // qemuPath to attach to
	dstFileName    string   // the prepared file
	nbdServCmd     exec.Cmd // command to serve the disk
}

func ParseDisk(spec string) (*Disk, error) {
	var channel string
	sectorSize := 0
	serialOpt := []string{}
	multipathed := false

	size, diskmap, err := util.ParseDiskSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to parse disk spec %q: %w", spec, err)
	}

	for key, value := range diskmap {
		switch key {
		case "channel":
			channel = value
		case "4k":
			sectorSize = 4096
		case "mpath":
			multipathed = true
		case "serial":
			value = "serial=" + value
			serialOpt = append(serialOpt, value)
		default:
			return nil, fmt.Errorf("invalid key %q", key)
		}
	}

	return &Disk{
		Size:          fmt.Sprintf("%dG", size),
		Channel:       channel,
		DeviceOpts:    serialOpt,
		SectorSize:    sectorSize,
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
	qemu         exec.Cmd
	architecture string
	tempdir      string
	swtpm        exec.Cmd
	// Helpers are child processes such as nbd or virtiofsd that should be lifecycle bound to qemu
	helpers            []exec.Cmd
	hostForwardedPorts []HostForwardPort

	journalPipe *os.File

	qmpSocket     *qmp.SocketMonitor
	qmpSocketPath string
}

// Signaled returns whether QEMU process was signaled.
func (inst *QemuInstance) Signaled() bool {
	return inst.qemu.Signaled()
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
	for _, p := range inst.helpers {
		if p != nil {
			p.Kill() //nolint // Ignore errors
		}
	}
	inst.helpers = nil

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
	switch inst.architecture {
	case "s390x", "aarch64":
		break
	default:
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
	// Get boot device for PXE boots
	for _, dev := range devs.Return {
		switch dev.Type {
		case "child<virtio-net-pci>", "child<virtio-net-ccw>":
			bootdev = filepath.Join("/machine/peripheral-anon", dev.Name)
		default:
			break
		}
	}
	// Get boot device for ISO boots and target block device
	for _, dev := range blkdevs.Return {
		devpath := filepath.Clean(strings.TrimSuffix(dev.DevicePath, "virtio-backend"))
		switch dev.Device {
		case "installiso":
			bootdev = devpath
		case "disk-1", "mpath10":
			primarydev = devpath
		case "mpath11":
			secondarydev = devpath
		case "":
			if dev.Inserted.NodeName == "installiso" {
				bootdev = devpath
			}
		default:
			break
		}
	}

	if bootdev == "" {
		return fmt.Errorf("Could not find boot device using QMP.\n"+
			"Full list of peripherals: %v.\n"+
			"Full list of block devices: %v.\n",
			devs.Return, blkdevs.Return)
	}

	if primarydev == "" {
		return fmt.Errorf("Could not find target disk using QMP.\n"+
			"Full list of block devices: %v.\n",
			blkdevs.Return)
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
// and sets the secondary device as primary. It expects that all block devices
// with device name disk-<N> are mirrors.
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
		if !dev.Removable && strings.HasPrefix(dev.Device, "disk-") {
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

// A directory mounted from the host into the guest, via 9p or virtiofs
type HostMount struct {
	src      string
	dest     string
	readonly bool
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

	// If set, use QEMU full emulation for the target architecture
	architecture string
	// MemoryMiB defaults to 1024 on most architectures, others it may be 2048
	MemoryMiB int
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
	usermodeNetworkingAddr    string
	RestrictNetworking        bool
	requestedHostForwardPorts []HostForwardPort
	additionalNics            int
	netbootP                  string
	netbootDir                string

	finalized bool
	diskID    uint
	disks     []*Disk
	// virtioSerialID is incremented for each device
	virtioSerialID uint
	// hostMounts is an array of directories mounted (via 9p or virtiofs) from the host
	hostMounts []HostMount
	// fds is file descriptors we own to pass to qemu
	fds []*os.File

	// IBM Secure Execution
	secureExecution bool
	ignitionPubKey  string
}

// NewQemuBuilder creates a new build for QEMU with default settings.
func NewQemuBuilder() *QemuBuilder {
	var defaultFirmware string
	switch coreosarch.CurrentRpmArch() {
	case "x86_64":
		defaultFirmware = "bios"
	case "aarch64":
		defaultFirmware = "uefi"
	default:
		defaultFirmware = ""
	}
	ret := QemuBuilder{
		Firmware:     defaultFirmware,
		Swtpm:        true,
		Pdeathsig:    true,
		Argv:         []string{},
		architecture: coreosarch.CurrentRpmArch(),
	}
	return &ret
}

func (builder *QemuBuilder) ensureTempdir() error {
	if builder.tempdir != "" {
		return nil
	}
	tempdir, err := os.MkdirTemp("/var/tmp", "mantle-qemu")
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

// Small wrapper around os.CreateTemp() to avoid leaking our tempdir to
// others.
func (builder *QemuBuilder) TempFile(pattern string) (*os.File, error) {
	if err := builder.ensureTempdir(); err != nil {
		return nil, err
	}
	return os.CreateTemp(builder.tempdir, pattern)
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
func virtio(arch, device, args string) string {
	var suffix string
	switch arch {
	case "x86_64", "ppc64le", "aarch64":
		suffix = "pci"
	case "s390x":
		suffix = "ccw"
	default:
		panic(fmt.Sprintf("RpmArch %s unhandled in virtio()", arch))
	}
	return fmt.Sprintf("virtio-%s-%s,%s", device, suffix, args)
}

// EnableUsermodeNetworking configure forwarding for all requested ports,
// via usermode network helpers.
func (builder *QemuBuilder) EnableUsermodeNetworking(h []HostForwardPort, usernetAddr string) {
	builder.UsermodeNetworking = true
	builder.requestedHostForwardPorts = h
	builder.usermodeNetworkingAddr = usernetAddr
}

func (builder *QemuBuilder) SetNetbootP(filename, dir string) {
	builder.UsermodeNetworking = true
	builder.netbootP = filename
	builder.netbootDir = dir
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
	if builder.usermodeNetworkingAddr != "" {
		netdev += ",net=" + builder.usermodeNetworkingAddr
	}
	if builder.netbootP != "" {
		// do an early stat so we fail with a nicer error now instead of in the VM
		if _, err := os.Stat(filepath.Join(builder.netbootDir, builder.netbootP)); err != nil {
			return err
		}
		tftpDir := ""
		relpath := ""
		if builder.netbootDir == "" {
			absPath, err := filepath.Abs(builder.netbootP)
			if err != nil {
				return err
			}
			tftpDir = filepath.Dir(absPath)
			relpath = filepath.Base(absPath)
		} else {
			absPath, err := filepath.Abs(builder.netbootDir)
			if err != nil {
				return err
			}
			tftpDir = absPath
			relpath = builder.netbootP
		}
		netdev += fmt.Sprintf(",tftp=%s,bootfile=/%s", tftpDir, relpath)
		builder.Append("-boot", "order=n")
	}

	builder.Append("-netdev", netdev, "-device", virtio(builder.architecture, "net", "netdev=eth0"))
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
		device := virtio(builder.architecture, "net", fmt.Sprintf("netdev=eth%s,mac=52:55:00:d1:56:%s", idSuffix, macSuffix))
		builder.Append("-netdev", netdev, "-device", device)
		macCounter++
	}

	return nil
}

// SetArchitecture enables qemu full emulation for the target architecture.
func (builder *QemuBuilder) SetArchitecture(arch string) error {
	switch arch {
	case "x86_64", "aarch64", "s390x", "ppc64le":
		builder.architecture = arch
		return nil
	}
	return fmt.Errorf("architecture %s not supported by coreos-assembler qemu", arch)
}

// SetSecureExecution enables qemu confidential guest support and adds hostkey to ignition config.
func (builder *QemuBuilder) SetSecureExecution(gpgkey string, hostkey string, config *conf.Conf) error {
	if supports, err := builder.supportsSecureExecution(); err != nil {
		return err
	} else if !supports {
		return fmt.Errorf("Secure Execution was requested but isn't supported/enabled")
	}
	if gpgkey == "" {
		return fmt.Errorf("Secure Execution was requested, but we don't have a GPG Public Key to encrypt the config")
	}

	if config != nil {
		if hostkey == "" {
			// dummy hostkey; this is good enough at least for the first boot (to prevent genprotimg from failing)
			dummy, err := builder.TempFile("hostkey.*")
			if err != nil {
				return fmt.Errorf("creating hostkey: %v", err)
			}
			c := exec.Command("openssl", "req", "-x509", "-sha512", "-nodes", "-days", "1", "-subj", "/C=US/O=IBM/CN=secex",
				"-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:secp521r1", "-out", dummy.Name())
			if err := c.Run(); err != nil {
				return fmt.Errorf("generating hostkey: %v", err)
			}
			hostkey = dummy.Name()
		}
		if contents, err := os.ReadFile(hostkey); err != nil {
			return fmt.Errorf("reading hostkey: %v", err)
		} else {
			config.AddFile("/etc/se-hostkeys/ibm-z-hostkey-1", string(contents), 0644)
		}
	}
	builder.secureExecution = true
	builder.ignitionPubKey = gpgkey
	builder.Append("-object", "s390-pv-guest,id=pv0", "-machine", "confidential-guest-support=pv0")
	return nil
}

func (builder *QemuBuilder) encryptIgnitionConfig() error {
	crypted, err := builder.TempFile("ignition_crypted.*")
	if err != nil {
		return fmt.Errorf("creating crypted config: %v", err)
	}
	c := exec.Command("gpg", "--recipient-file", builder.ignitionPubKey, "--yes", "--output", crypted.Name(), "--armor", "--encrypt", builder.ConfigFile)
	if err := c.Run(); err != nil {
		return fmt.Errorf("encrypting %s: %v", crypted.Name(), err)
	}
	builder.ConfigFile = crypted.Name()
	return nil
}

// MountHost sets up a mount point from the host to guest.
// Note that virtiofs does not currently support read-only mounts (which is really surprising!).
// We do mount it read-only by default in the guest, however.
func (builder *QemuBuilder) MountHost(source, dest string, readonly bool) {
	builder.hostMounts = append(builder.hostMounts, HostMount{src: source, dest: dest, readonly: readonly})
}

// supportsFwCfg if the target system supports injecting
// Ignition via the qemu -fw_cfg option.
func (builder *QemuBuilder) supportsFwCfg() bool {
	switch builder.architecture {
	case "s390x", "ppc64le":
		return false
	}
	return true
}

// supportsSecureExecution if s390x host (zKVM/LPAR) has "Secure Execution for Linux" feature enabled
func (builder *QemuBuilder) supportsSecureExecution() (bool, error) {
	if builder.architecture != "s390x" {
		return false, nil
	}
	content, err := os.ReadFile("/sys/firmware/uv/prot_virt_host")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading protvirt flag: %v", err)
	}
	if len(content) < 1 {
		return false, nil
	}
	enabled := content[0] == '1'
	return enabled, nil
}

// supportsSwtpm if the target system supports a virtual TPM device
func (builder *QemuBuilder) supportsSwtpm() bool {
	switch builder.architecture {
	case "s390x":
		// s390x does not support a backend for TPM
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

func newGuestfish(arch, diskImagePath string, diskSectorSize int) (*coreosGuestfish, error) {
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

	// Hack to run with a wrapper on older P8 hardware running RHEL7
	switch arch {
	case "ppc64le":
		u := unix.Utsname{}
		if err := unix.Uname(&u); err != nil {
			return nil, errors.Wrapf(err, "detecting kernel information")
		}
		if strings.Contains(fmt.Sprintf("%s", u.Release), "el7") {
			plog.Infof("Detected el7. Running using libguestfs-ppc64le-wrapper.sh")
			cmd.Env = append(cmd.Env, "LIBGUESTFS_HV=/usr/lib/coreos-assembler/libguestfs-ppc64le-wrapper.sh")
		}
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
	buf, err := io.ReadAll(stdout)
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

	rootfs, err := findLabel("root", pid)
	if err != nil {
		return nil, errors.Wrapf(err, "guestfish command failed to find root label")
	}
	if err := exec.Command("guestfish", remote, "mount", rootfs, "/").Run(); err != nil {
		return nil, errors.Wrapf(err, "guestfish root mount failed")
	}

	bootfs, err := findLabel("boot", pid)
	if err != nil {
		return nil, errors.Wrapf(err, "guestfish command failed to find boot label")
	}

	if err := exec.Command("guestfish", remote, "mount", bootfs, "/boot").Run(); err != nil {
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
func setupPreboot(arch, confPath, firstbootkargs, kargs string, diskImagePath string, diskSectorSize int) error {
	gf, err := newGuestfish(arch, diskImagePath, diskSectorSize)
	if err != nil {
		return err
	}
	defer gf.destroy()

	if confPath != "" {
		if err := exec.Command("guestfish", gf.remote, "mkdir-p", "/boot/ignition").Run(); err != nil {
			return errors.Wrapf(err, "guestfish directory creation failed")
		}

		if err := exec.Command("guestfish", gf.remote, "upload", confPath, "/boot"+fileRemoteLocation).Run(); err != nil {
			return errors.Wrapf(err, "guestfish upload failed")
		}
	}

	// See /boot/grub2/grub.cfg
	if firstbootkargs != "" {
		grubStr := fmt.Sprintf("set ignition_network_kcmdline=\"%s\"\n", firstbootkargs)
		if err := exec.Command("guestfish", gf.remote, "write", "/boot/ignition.firstboot", grubStr).Run(); err != nil {
			return errors.Wrapf(err, "guestfish write")
		}
	}
	// Parsing BLS
	var linux string
	var initrd string
	var allkargs string
	zipl_sync := arch == "s390x" && (firstbootkargs != "" || kargs != "")
	if kargs != "" || zipl_sync {
		confpathout, err := exec.Command("guestfish", gf.remote, "glob-expand", "/boot/loader/entries/ostree*conf").Output()
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
				allkargs = strings.TrimPrefix(line, "options ")
			} else if strings.HasPrefix(line, "linux ") {
				linux = "/boot" + strings.TrimPrefix(line, "linux ")
			} else if strings.HasPrefix(line, "initrd ") {
				initrd = "/boot" + strings.TrimPrefix(line, "initrd ")
			}
			buf.Write([]byte(line))
			buf.Write([]byte("\n"))
		}
		if kargs != "" {
			if err := exec.Command("guestfish", gf.remote, "write", confpath, buf.String()).Run(); err != nil {
				return errors.Wrapf(err, "writing bootloader config")
			}
		}
	}

	// s390x requires zipl to update low-level data on block device
	if zipl_sync {
		allkargs = strings.TrimSpace(allkargs + " ignition.firstboot " + firstbootkargs)
		if err := runZipl(gf, linux, initrd, allkargs); err != nil {
			return errors.Wrapf(err, "running zipl")
		}
	}

	if err := exec.Command("guestfish", gf.remote, "umount-all").Run(); err != nil {
		return errors.Wrapf(err, "guestfish umount failed")
	}
	return nil
}

func runZipl(gf *coreosGuestfish, linux string, initrd string, options string) error {
	// Detecting ostree commit
	deploy, err := exec.Command("guestfish", gf.remote, "glob-expand", "/ostree/deploy/*/deploy/*.0").Output()
	if err != nil {
		return errors.Wrapf(err, "finding deploy path")
	}
	sysroot := strings.TrimSpace(string(deploy))
	// Saving cmdline
	if err := exec.Command("guestfish", gf.remote, "write", "/boot/zipl.cmdline", options+"\n").Run(); err != nil {
		return errors.Wrapf(err, "writing zipl cmdline")
	}
	// Bind-mounting for chroot
	if err := exec.Command("guestfish", gf.remote, "debug", "sh", fmt.Sprintf("'mount -t devtmpfs none /sysroot/%s/dev'", sysroot)).Run(); err != nil {
		return errors.Wrapf(err, "bind-mounting devtmpfs")
	}
	if err := exec.Command("guestfish", gf.remote, "debug", "sh", fmt.Sprintf("'mount -t proc none /sysroot/%s/proc'", sysroot)).Run(); err != nil {
		return errors.Wrapf(err, "bind-mounting /proc")
	}
	if err := exec.Command("guestfish", gf.remote, "debug", "sh", fmt.Sprintf("'mount -o bind /sysroot/boot /sysroot/%s/boot'", sysroot)).Run(); err != nil {
		return errors.Wrapf(err, "bind-mounting /boot")
	}
	// chroot zipl
	cmd := exec.Command("guestfish", gf.remote, "debug", "sh", fmt.Sprintf("'chroot /sysroot/%s /sbin/zipl -i %s -r %s -p /boot/zipl.cmdline -t /boot'", sysroot, linux, initrd))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "running zipl")
	}
	// clean-up
	if err := exec.Command("guestfish", gf.remote, "rm-f", "/boot/zipl.cmdline").Run(); err != nil {
		return errors.Wrapf(err, "writing zipl cmdline")
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
	tmpf, err := os.CreateTemp(builder.tempdir, "disk")
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
			} else if strings.HasSuffix(backingFile, "raw") {
				format = "raw"
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
		// Only try to inject config if it hasn't already been injected somewhere
		// else, which can happen when running an ISO install.
		if !builder.configInjected {
			// If the board doesn't support -fw_cfg or we were explicitly
			// requested, inject via libguestfs on the primary disk.
			if err := builder.renderIgnition(); err != nil {
				return errors.Wrapf(err, "rendering ignition")
			}
			requiresInjection := builder.ConfigFile != "" && builder.ForceConfigInjection
			if requiresInjection || builder.AppendFirstbootKernelArgs != "" || builder.AppendKernelArgs != "" {
				if err := setupPreboot(builder.architecture, builder.ConfigFile, builder.AppendFirstbootKernelArgs, builder.AppendKernelArgs,
					disk.dstFileName, disk.SectorSize); err != nil {
					return errors.Wrapf(err, "ignition injection with guestfs failed")
				}
				builder.configInjected = true
			}
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

	id := fmt.Sprintf("disk-%d", builder.diskID)

	// Avoid file locking detection, and the disks we create
	// here are always currently ephemeral.
	defaultDiskOpts := "auto-read-only=off,cache=unsafe"
	if len(disk.DriveOpts) > 0 {
		defaultDiskOpts += "," + strings.Join(disk.DriveOpts, ",")
	}

	if disk.MultiPathDisk {
		// Fake a NVME device with a fake WWN. All these attributes are needed in order
		// to trick multipath-tools that this is a "real" multipath device.
		// Each disk is presented on its own controller.

		// The WWN needs to be a unique uint64 number
		wwn := rand.Uint64()

		var bus string
		switch builder.architecture {
		case "x86_64", "ppc64le", "aarch64":
			bus = "pci"
		case "s390x":
			bus = "ccw"
		default:
			panic(fmt.Sprintf("Mantle doesn't know which bus type to use on %s", builder.architecture))
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
			builder.Append("-device", virtio(builder.architecture, "blk", fmt.Sprintf("drive=%s%s", id, opts)))
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
		if disk, err := ParseDisk(spec); err != nil {
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
	if builder.MemoryMiB == 0 {
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
		switch builder.architecture {
		case "aarch64", "s390x", "ppc64le":
			memory = 2048
		}
		builder.MemoryMiB = memory
	}
	builder.finalized = true
}

// Append appends additional arguments for QEMU.
func (builder *QemuBuilder) Append(args ...string) {
	builder.Argv = append(builder.Argv, args...)
}

// baseQemuArgs takes a board and returns the basic qemu
// arguments needed for the current architecture.
func baseQemuArgs(arch string, memoryMiB int) ([]string, error) {
	// memoryDevice is the object identifier we use for the backing RAM
	const memoryDevice = "mem"

	kvm := true
	hostArch := coreosarch.CurrentRpmArch()
	// The machine argument needs to reference our memory device; see below
	machineArg := "memory-backend=" + memoryDevice
	accel := "accel=kvm"
	if _, ok := os.LookupEnv("COSA_NO_KVM"); ok || hostArch != arch {
		accel = "accel=tcg"
		kvm = false
	}
	machineArg += "," + accel
	var ret []string
	switch arch {
	case "x86_64":
		ret = []string{
			"qemu-system-x86_64",
			"-machine", machineArg,
		}
	case "aarch64":
		ret = []string{
			"qemu-system-aarch64",
			"-machine", "virt,gic-version=max," + machineArg,
		}
	case "s390x":
		ret = []string{
			"qemu-system-s390x",
			"-machine", "s390-ccw-virtio," + machineArg,
		}
	case "ppc64le":
		ret = []string{
			"qemu-system-ppc64",
			// kvm-type=HV ensures we use bare metal KVM and not "user mode"
			// https://qemu.readthedocs.io/en/latest/system/ppc/pseries.html#switching-between-the-kvm-pr-and-kvm-hv-kernel-module
			"-machine", "pseries,kvm-type=HV," + machineArg,
		}
	default:
		return nil, fmt.Errorf("architecture %s not supported for qemu", arch)
	}
	if kvm {
		ret = append(ret, "-cpu", "host")
	} else {
		if arch == "x86_64" {
			// the default qemu64 CPU model does not support x86_64_v2
			// causing crashes on EL9+ kernels
			// see https://bugzilla.redhat.com/show_bug.cgi?id=2060839
			ret = append(ret, "-cpu", "Nehalem")
		}
	}
	// And define memory using a memfd (in shared mode), which is needed for virtiofs
	ret = append(ret, "-object", fmt.Sprintf("memory-backend-memfd,id=%s,size=%dM,share=on", memoryDevice, memoryMiB))
	ret = append(ret, "-m", fmt.Sprintf("%d", memoryMiB))
	return ret, nil
}

func (builder *QemuBuilder) setupUefi(secureBoot bool) error {
	switch coreosarch.CurrentRpmArch() {
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
		vars, err := os.CreateTemp("", "mantle-qemu")
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
			return fmt.Errorf("architecture %s doesn't have support for secure boot in kola", coreosarch.CurrentRpmArch())
		}
		vars, err := os.CreateTemp("", "mantle-qemu")
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
		builder.Append("-drive", "file=/usr/share/edk2/aarch64/QEMU_EFI-silent-pflash.raw,if=pflash,format=raw,unit=0,readonly=on,auto-read-only=off")
		builder.Append("-drive", fmt.Sprintf("file=%s,if=pflash,format=raw,unit=1,readonly=off,auto-read-only=off", fdset))
	default:
		panic(fmt.Sprintf("Architecture %s doesn't have support for UEFI in qemu.", coreosarch.CurrentRpmArch()))
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
		allargs := fmt.Sprintf("console=%s %s", consoleKernelArgument[coreosarch.CurrentRpmArch()], builder.AppendKernelArgs)
		instCmdKargs := exec.Command("coreos-installer", "iso", "kargs", "modify", "--append", allargs, isoEmbeddedPath)
		var stderrb bytes.Buffer
		instCmdKargs.Stderr = &stderrb
		if err := instCmdKargs.Run(); err != nil {
			// Don't make this a hard error if it's just for console; we
			// may be operating on an old live ISO
			if len(builder.AppendKernelArgs) > 0 {
				return errors.Wrapf(err, "running `coreos-installer iso kargs modify`; old CoreOS ISO?")
			}
			// Only actually emit a warning if we expected it to be supported
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
	switch coreosarch.CurrentRpmArch() {
	case "s390x":
		if builder.isoAsDisk {
			// we could do it, but boot would fail
			return errors.New("cannot attach ISO as disk; no hybrid ISO on this arch")
		}
		builder.Append("-blockdev", "file,node-name=installiso,filename="+builder.iso.path,
			"-device", "virtio-scsi", "-device", "scsi-cd,drive=installiso,bootindex=2")
	case "ppc64le", "aarch64":
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
			builder.Append("-device", virtio(builder.architecture, "blk", "drive=installiso"+bootindexStr))
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
//   - The first parameter is a poitner to the configuration of the target VM.
//   - The second parameter is an optional queryArguments to filter the stream -
//     see `man journalctl` for more information.
//   - The return value is a file stream which will be newline-separated JSON.
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

// createVirtiofsCmd returns a new command instance configured to launch virtiofsd.
func createVirtiofsCmd(directory, socketPath string) exec.Cmd {
	args := []string{"--sandbox", "none", "--socket-path", socketPath, "--shared-dir", "."}
	// Work around https://gitlab.com/virtio-fs/virtiofsd/-/merge_requests/197
	if os.Getuid() == 0 {
		args = append(args, "--modcaps=-mknod:-setfcap")
	}
	// We don't need seccomp filtering; we trust our workloads. This incidentally
	// works around issues like https://gitlab.com/virtio-fs/virtiofsd/-/merge_requests/200.
	args = append(args, "--seccomp=none")
	cmd := exec.Command("/usr/libexec/virtiofsd", args...)
	// This sets things up so that the `.` we passed in the arguments is the target directory
	cmd.Dir = directory
	// Quiet the daemon by default
	cmd.Env = append(cmd.Env, "RUST_LOG=ERROR")
	// But we do want to see errors
	cmd.Stderr = os.Stderr
	// Like other processes, "lifecycle bind" it to us
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	return cmd
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

	argv, err := baseQemuArgs(builder.architecture, builder.MemoryMiB)
	if err != nil {
		return nil, err
	}

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
	case "":
		// Nothing to do, use qemu default
	case "uefi":
		if err := builder.setupUefi(false); err != nil {
			return nil, err
		}
	case "uefi-secure":
		if err := builder.setupUefi(true); err != nil {
			return nil, err
		}
	case "bios":
		if coreosarch.CurrentRpmArch() != "x86_64" {
			return nil, fmt.Errorf("unknown firmware: %s", builder.Firmware)
		}
	default:
		return nil, fmt.Errorf("unknown firmware: %s", builder.Firmware)
	}

	// We always provide a random source
	argv = append(argv, "-object", "rng-random,filename=/dev/urandom,id=rng0",
		"-device", virtio(builder.architecture, "rng", "rng=rng0"))
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
			serial := "ignition"
			if builder.secureExecution {
				// SE case: we have to encrypt the config and attach it with 'serial=ignition_crypted'
				if err := builder.encryptIgnitionConfig(); err != nil {
					return nil, err
				}
				serial = "ignition_crypted"
			}
			// Alternative to fw_cfg, should be generally usable on all arches,
			// especially those without fw_cfg support.
			// See https://github.com/coreos/ignition/pull/905
			builder.Append("-drive", fmt.Sprintf("if=none,id=ignition,format=raw,file=%s,readonly=on", builder.ConfigFile),
				"-device", fmt.Sprintf("virtio-blk,serial=%s,drive=ignition", serial))
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
			inst.helpers = append(inst.helpers, cmd)
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
		switch builder.architecture {
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

	// Process virtiofs mounts
	if len(builder.hostMounts) > 0 {
		if err := builder.ensureTempdir(); err != nil {
			return nil, err
		}

		plog.Debug("creating virtiofs helpers")

		// Spawn off a virtiofsd helper per mounted path
		virtiofsHelpers := make(map[string]exec.Cmd)
		for i, hostmnt := range builder.hostMounts {
			// By far the most common failure to spawn virtiofsd will be a typo'd source directory,
			// so let's synchronously check that ourselves here.
			if _, err := os.Stat(hostmnt.src); err != nil {
				return nil, fmt.Errorf("failed to access virtiofs source directory %s", hostmnt.src)
			}
			virtiofsChar := fmt.Sprintf("virtiofschar%d", i)
			virtiofsdSocket := filepath.Join(builder.tempdir, fmt.Sprintf("virtiofsd-%d.sock", i))
			builder.Append("-chardev", fmt.Sprintf("socket,id=%s,path=%s", virtiofsChar, virtiofsdSocket))
			builder.Append("-device", fmt.Sprintf("vhost-user-fs-pci,queue-size=1024,chardev=%s,tag=%s", virtiofsChar, hostmnt.dest))
			plog.Debugf("creating virtiofs helper for %s", hostmnt.src)
			// TODO: Honor hostmnt.readonly somehow here (add an option to virtiofsd)
			p := createVirtiofsCmd(hostmnt.src, virtiofsdSocket)
			if err := p.Start(); err != nil {
				return nil, fmt.Errorf("failed to start virtiofsd")
			}
			virtiofsHelpers[virtiofsdSocket] = p
		}
		// Loop waiting for the sockets to appear
		err := util.RetryUntilTimeout(10*time.Minute, 1*time.Second, func() error {
			found := []string{}
			for sockpath := range virtiofsHelpers {
				if _, err := os.Stat(sockpath); err == nil {
					found = append(found, sockpath)
				}
			}
			for _, sockpath := range found {
				helper := virtiofsHelpers[sockpath]
				inst.helpers = append(inst.helpers, helper)
				delete(virtiofsHelpers, sockpath)
			}
			if len(virtiofsHelpers) == 0 {
				return nil
			}
			waitingFor := []string{}
			for socket := range virtiofsHelpers {
				waitingFor = append(waitingFor, socket)
			}
			return fmt.Errorf("waiting for virtiofsd sockets: %s", strings.Join(waitingFor, " "))
		})
		if err != nil {
			return nil, err
		}
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
	inst.architecture = builder.architecture

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

	// Hacky code to test https://github.com/openshift/os/pull/1346
	if timeout, ok := os.LookupEnv("COSA_TEST_CDROM_UNPLUG"); ok {
		val, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, err
		}
		go func() {
			devs, err := inst.listDevices()
			if err != nil {
				plog.Error("failed to list devices")
				return
			}

			var cdrom string
			for _, dev := range devs.Return {
				switch dev.Type {
				case "child<scsi-cd>":
					cdrom = filepath.Join("/machine/peripheral-anon", dev.Name)
				default:
					break
				}
			}
			if cdrom == "" {
				plog.Errorf("failed to get scsi-cd id")
				return
			}

			plog.Debugf("get cdrom id %s", cdrom)
			time.Sleep(val)
			if err := inst.deleteBlockDevice(cdrom); err != nil {
				plog.Errorf("failed to delete block device: %s", cdrom)
				return
			}
			plog.Info("delete cdrom")
		}()
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
