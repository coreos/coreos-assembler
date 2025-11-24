package iso

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
)

const (
	installTimeoutMins = 12
	// https://github.com/coreos/fedora-coreos-config/pull/2544
	liveISOFromRAMKarg = "coreos.liveiso.fromram"
)

type IsoTestOpts struct {
	// Flags().BoolVarP(&instInsecure, "inst-insecure", "S", false, "Do not verify signature on metal image")
	instInsecure bool
	// Flags().BoolVar(&console, "console", false, "Connect qemu console to terminal, turn off automatic initramfs failure checking")
	console          bool
	addNmKeyfile     bool
	enable4k         bool
	enableMultipath  bool
	isOffline        bool
	isISOFromRAM     bool
	isMiniso         bool
	enableUefi       bool
	enableUefiSecure bool
	enableIbft       bool
	manual           bool
	pxeAppendRootfs  bool
}

func (o *IsoTestOpts) SetInsecureOnDevBuild() {
	// Ignore signing verification by default when running with development build
	// https://github.com/coreos/fedora-coreos-tracker/issues/908
	if kola.QEMUOptions.InstInsecure || strings.Contains(kola.CosaBuild.Meta.BuildID, ".dev.") {
		o.instInsecure = true
		//fmt.Printf("Detected development build; disabling signature verification\n")
	}
}

func isoTest(name string, run func(c cluster.TestCluster), arch []string) *register.Test {
	return &register.Test{
		Run:           run,
		ClusterSize:   0,
		Name:          "iso." + name,
		Timeout:       installTimeoutMins * time.Minute,
		Platforms:     []string{"qemu"},
		Architectures: arch,
	}
}

func newBaseQemuBuilder(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, error) {
	builder := qemu.NewMetalQemuBuilderDefault()
	if opts.enableUefiSecure {
		builder.Firmware = "uefi-secure"
	} else if opts.enableUefi {
		builder.Firmware = "uefi"
	}

	if err := os.MkdirAll(outdir, 0755); err != nil {
		return nil, err
	}

	builder.InheritConsole = opts.console
	if !opts.console {
		builder.ConsoleFile = filepath.Join(outdir, "console.txt")
	}

	if kola.QEMUOptions.Memory != "" {
		parsedMem, err := strconv.ParseInt(kola.QEMUOptions.Memory, 10, 32)
		if err != nil {
			return nil, err
		}
		builder.MemoryMiB = int(parsedMem)
	}

	// increase the memory for pxe tests with appended rootfs in the initrd
	// we were bumping up into the 4GiB limit in RHCOS/c9s
	// pxe-offline-install.rootfs-appended.bios tests
	if opts.pxeAppendRootfs && builder.MemoryMiB < 5120 {
		builder.MemoryMiB = 5120
	}

	return builder, nil
}

func newQemuBuilder(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, err := newBaseQemuBuilder(opts, outdir)
	if err != nil {
		return nil, nil, err
	}

	config, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return nil, nil, err
	}

	err = forwardJournal(outdir, builder, config)
	if err != nil {
		return nil, nil, err
	}

	return builder, config, nil
}

func forwardJournal(outdir string, builder *platform.QemuBuilder, config *conf.Conf) error {
	journalPipe, err := builder.VirtioJournal(config, "")
	if err != nil {
		return err
	}
	journalOut, err := os.OpenFile(filepath.Join(outdir, "journal.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	go func() {
		_, err := io.Copy(journalOut, journalPipe)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	return nil
}

func newQemuBuilderWithDisk(opts IsoTestOpts, outdir string) (*platform.QemuBuilder, *conf.Conf, error) {
	builder, config, err := newQemuBuilder(opts, outdir)

	if err != nil {
		return nil, nil, err
	}

	sectorSize := 0
	if opts.enable4k {
		sectorSize = 4096
	}

	disk := platform.Disk{
		Size:          "12G", // Arbitrary
		SectorSize:    sectorSize,
		MultiPathDisk: opts.enableMultipath,
	}

	//TBD: see if we can remove this and just use AddDisk and inject bootindex during startup
	if coreosarch.CurrentRpmArch() == "s390x" || coreosarch.CurrentRpmArch() == "aarch64" {
		// s390x and aarch64 need to use bootindex as they don't support boot once
		if err := builder.AddDisk(&disk); err != nil {
			return nil, nil, err
		}
	} else {
		if err := builder.AddPrimaryDisk(&disk); err != nil {
			return nil, nil, err
		}
	}

	return builder, config, nil
}

func CheckTestOutput(output *os.File, expected []string) error {
	reader := bufio.NewReader(output)
	for _, exp := range expected {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// this may be from QEMU getting killed or exiting; wait a bit
				// to give a chance for .Wait() above to feed the channel with a
				// better error
				time.Sleep(1 * time.Second)
				return fmt.Errorf("got EOF from completion channel, %s expected", exp)
			} else {
				return errors.Wrapf(err, "reading from completion channel")
			}
		}
		line = strings.TrimSpace(line)
		if line != exp {
			return fmt.Errorf("unexpected string from completion channel: %q, expected: %q", line, exp)
		}
	}
	return nil
}

func awaitCompletion(c cluster.TestCluster, inst *platform.QemuInstance, console bool, outdir string, qchan *os.File, booterrchan chan error, expected []string) error {
	ctx := c.Context()

	errchan := make(chan error)
	go func() {
		timeout := (time.Duration(installTimeoutMins*(100+kola.Options.ExtendTimeoutPercent)) * time.Minute) / 100
		time.Sleep(timeout)
		errchan <- fmt.Errorf("timed out after %v", timeout)
	}()
	if !console {
		go func() {
			errBuf, err := inst.WaitIgnitionError(ctx)
			if err == nil {
				if errBuf != "" {
					c.Logf("entered emergency.target in initramfs")
					path := filepath.Join(outdir, "ignition-virtio-dump.txt")
					if err := os.WriteFile(path, []byte(errBuf), 0644); err != nil {
						c.Errorf("Failed to write journal: %v", err)
					}
					err = platform.ErrInitramfsEmergency
				}
			}
			if err != nil {
				errchan <- err
			}
		}()
	}
	go func() {
		err := inst.Wait()
		// only one Wait() gets process data, so also manually check for signal
		//plog.Debugf("qemu exited err=%v", err)
		if err == nil && inst.Signaled() {
			err = errors.New("process killed")
		}
		if err != nil {
			errchan <- errors.Wrapf(err, "QEMU unexpectedly exited while awaiting completion")
		}
		time.Sleep(1 * time.Minute)
		errchan <- fmt.Errorf("QEMU exited; timed out waiting for completion")
	}()
	go func() {
		errchan <- CheckTestOutput(qchan, expected)
	}()
	go func() {
		//check for error when switching boot order
		if booterrchan != nil {
			if err := <-booterrchan; err != nil {
				errchan <- err
			}
		}
	}()
	err := <-errchan
	return err
}

var liveOKSignal = "live-test-OK"
var liveSignalOKUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Live ISO Completion
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
[Install]
# for install tests
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`, liveOKSignal)

var signalCompleteString = "coreos-installer-test-OK"
var signalCompletionUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Completion
Requires=dev-virtio\\x2dports-testisocompletion.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=multi-user.target`, signalCompleteString)

var signalEmergencyString = "coreos-installer-test-entered-emergency-target"
var signalFailureUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Failure
Requires=dev-virtio\\x2dports-testisocompletion.device
DefaultDependencies=false
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion && systemctl poweroff'
[Install]
RequiredBy=emergency.target`, signalEmergencyString)

var multipathedRoot = `[Unit]
Description=TestISO Verify Multipathed Root
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c 'lsblk -pno NAME "/dev/mapper/$(multipath -l -v 1)" | grep -qw "$(findmnt -nvr /sysroot -o SOURCE)"'
[Install]
RequiredBy=multi-user.target`

var checkNoIgnition = `
[Unit]
Description=TestISO Verify No Ignition Config
OnFailure=emergency.target
OnFailureJobMode=isolate
Before=coreos-test-installer.service
After=coreos-ignition-firstboot-complete.service
RequiresMountsFor=/boot
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '[ ! -e /boot/ignition ]'
[Install]
RequiredBy=multi-user.target`

// This test is broken. Please fix!
// https://github.com/coreos/coreos-assembler/issues/3554
var verifyNoEFIBootEntry = `
[Unit]
Description=TestISO Verify No EFI Boot Entry
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionPathExists=/sys/firmware/efi
Before=live-signal-ok.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '! efibootmgr -v | grep -E "(HD|CDROM)\("'
[Install]
# for install tests
RequiredBy=coreos-installer.target
# for iso-as-disk
RequiredBy=multi-user.target`

// Verify that the volume ID is the OS name. See also
// https://github.com/openshift/assisted-image-service/pull/477.
// This is the same as the LABEL of the block device for ISO9660. See
// https://github.com/util-linux/util-linux/blob/643bdae8e38055e36acf2963c3416de206081507/libblkid/src/superblocks/iso9660.c#L366-L377
var verifyIsoVolumeId = `
[Unit]
Description=Verify ISO Volume ID
OnFailure=emergency.target
OnFailureJobMode=isolate
# only if we're actually mounting the ISO
ConditionPathIsMountPoint=/run/media/iso
[Service]
Type=oneshot
RemainAfterExit=yes
# the backing device name is arch-dependent, but we know it's mounted on /run/media/iso
ExecStart=bash -c "[[ $(findmnt -no LABEL /run/media/iso) == %s-* ]]"
[Install]
RequiredBy=coreos-installer.target`

// Unit to check that /run/media/iso is not mounted when
// coreos.liveiso.fromram kernel argument is passed
var isoNotMountedUnit = `
[Unit]
Description=Verify ISO is not mounted when coreos.liveiso.fromram
OnFailure=emergency.target
OnFailureJobMode=isolate
ConditionKernelCommandLine=coreos.liveiso.fromram
[Service]
Type=oneshot
StandardOutput=kmsg+console
StandardError=kmsg+console
RemainAfterExit=yes
# Would like to use SuccessExitStatus but it doesn't support what
# we want: https://github.com/systemd/systemd/issues/10297#issuecomment-1672002635
ExecStart=bash -c "if mountpoint /run/media/iso 2>/dev/null; then exit 1; fi"
[Install]
RequiredBy=coreos-installer.target`

var nmConnectionId = "CoreOS DHCP"
var nmConnectionFile = "coreos-dhcp.nmconnection"
var nmConnection = fmt.Sprintf(`[connection]
id=%s
type=ethernet
# add wait-device-timeout here so we make sure NetworkManager-wait-online.service will
# wait for a device to be present before exiting. See
# https://github.com/coreos/fedora-coreos-tracker/issues/1275#issuecomment-1231605438
wait-device-timeout=20000

[ipv4]
method=auto
`, nmConnectionId)

var nmstateConfigFile = "/etc/nmstate/br-ex.yml"
var nmstateConfig = `interfaces:
 - name: br-ex
   type: linux-bridge
   state: up
   ipv4:
     enabled: false
   ipv6:
     enabled: false
   bridge:
     port: []
`

// This is used to verify *both* the live and the target system in the `--add-nm-keyfile` path.
var verifyNmKeyfile = fmt.Sprintf(`[Unit]
Description=TestISO Verify NM Keyfile Propagation
OnFailure=emergency.target
OnFailureJobMode=isolate
Wants=network-online.target
After=network-online.target
Before=live-signal-ok.service
Before=coreos-test-installer.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/journalctl -u nm-initrd --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/journalctl -u NetworkManager --no-pager --grep "policy: set '%[1]s' (.*) as default .* routing and DNS"
ExecStart=/usr/bin/grep "%[1]s" /etc/NetworkManager/system-connections/%[2]s
# Also verify nmstate config
ExecStart=/usr/bin/nmcli c show br-ex
[Install]
# for live system
RequiredBy=coreos-installer.target
# for target system
RequiredBy=multi-user.target`, nmConnectionId, nmConnectionFile)
