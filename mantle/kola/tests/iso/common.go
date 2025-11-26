package iso

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
)

const (
	installTimeoutMins = 12
	// defaultQemuHostIPv4 is documented in `man qemu-kvm`, under the `-netdev` option
	defaultQemuHostIPv4 = "10.0.2.2"
)

// This object gets serialized to YAML and fed to coreos-installer:
// https://coreos.github.io/coreos-installer/customizing-install/#config-file-format
type coreosInstallerConfig struct {
	ImageURL     string   `yaml:"image-url,omitempty"`
	IgnitionFile string   `yaml:"ignition-file,omitempty"`
	Insecure     bool     `yaml:"insecure,omitempty"`
	AppendKargs  []string `yaml:"append-karg,omitempty"`
	CopyNetwork  bool     `yaml:"copy-network,omitempty"`
	DestDevice   string   `yaml:"dest-device,omitempty"`
	Console      []string `yaml:"console,omitempty"`
}

// Sometimes the logs that stream from various virtio streams can be
// incomplete because they depend on services inside the guest.
// When you are debugging earlyboot/initramfs issues this can be
// problematic. Let's add a hook here to enable more debugging.
func renderCosaTestIsoDebugKargs() []string {
	if _, ok := os.LookupEnv("COSA_TESTISO_DEBUG"); ok {
		return []string{"systemd.log_color=0", "systemd.log_level=debug",
			"systemd.journald.forward_to_console=1",
			"systemd.journald.max_level_console=debug"}
	} else {
		return []string{}
	}
}

func absSymlink(src, dest string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return os.Symlink(src, dest)
}

func setupMetalImage(builddir, metalimg, destdir string) (string, error) {
	if err := absSymlink(filepath.Join(builddir, metalimg), filepath.Join(destdir, metalimg)); err != nil {
		return "", err
	}
	return metalimg, nil
}

// startHTTPServer starts an HTTP file server in a goroutine and returns the server.
// The caller is responsible for closing the server.
func startHTTPServer(listener net.Listener, dir string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	server := &http.Server{Handler: mux}
	go func() {
		server.Serve(listener)
	}()
	return server
}

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

func getIsoTestOpts(testName string) IsoTestOpts {
	opts := IsoTestOpts{}

	// Parse test name to determine options
	if strings.Contains(testName, "4k") {
		opts.enable4k = true
	}
	if strings.Contains(testName, "uefi-secure") {
		opts.enableUefiSecure = true
	} else if strings.Contains(testName, "uefi") {
		opts.enableUefi = true
	}
	if strings.Contains(testName, "mpath") {
		opts.enableMultipath = true
	}
	if strings.Contains(testName, "offline") {
		opts.isOffline = true
	}
	if strings.Contains(testName, "fromram") {
		opts.isISOFromRAM = true
	}
	if strings.Contains(testName, "miniso") {
		opts.isMiniso = true
	}
	if strings.Contains(testName, ".nm") {
		opts.addNmKeyfile = true
	}
	if strings.Contains(testName, "rootfs-appended") {
		opts.pxeAppendRootfs = true
	}
	if strings.Contains(testName, "ibft") {
		opts.enableIbft = true
	}
	if strings.Contains(testName, "manual") {
		opts.manual = true
	}

	opts.instInsecure = kola.QEMUOptions.InstInsecure || IsDevBuild()
	return opts
}

func IsDevBuild() bool {
	// Ignore signing verification by default when running with development build
	// https://github.com/coreos/fedora-coreos-tracker/issues/908
	if kola.CosaBuild != nil && strings.Contains(kola.CosaBuild.Meta.BuildID, ".dev.") {
		fmt.Printf("Detected development build; disabling signature verification\n")
		return true
	}
	return false
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

// Reads from a virtio channel and validates that the expected
// strings are received in order. Returns an error if EOF is encountered, a read
// error occurs, or an unexpected string is received.
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
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
[Install]
RequiredBy=multi-user.target`, signalCompleteString)

// signalFailureUnit also ensures that the journal is dumped to the virtio port if the system
// enters emergency.target. This is needed when running from the live ISO and coreos-installer
// fails, because ignition-virtio-dump-journal.service is no longer enabled in that context:
// after switch root occurs, coreos-installer runs from the real root filesystem rather than
// the initramfs. Using this unit guarantees we can still catch errors in platform.StartMachine.
var signalEmergencyString = "coreos-installer-test-entered-emergency-target"
var signalFailureUnit = fmt.Sprintf(`
[Unit]
Description=TestISO Signal Failure
Requires=dev-virtio\\x2dports-testisocompletion.device
DefaultDependencies=false
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/testisocompletion'
ExecStart=/bin/bash -c '\
if ! systemctl is-enabled ignition-virtio-dump-journal.service >/dev/null 2>&1; then \
  exec /usr/lib/dracut/modules.d/99emergency-shell-setup/ignition-virtio-dump-journal.sh; \
fi'
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

var bootStartedSignal = "boot-started-OK"
var bootStartedUnit = fmt.Sprintf(`[Unit]
Description=TestISO Boot Started
Requires=dev-virtio\\x2dports-bootstarted.device
OnFailure=emergency.target
OnFailureJobMode=isolate
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c '/usr/bin/echo %s >/dev/virtio-ports/bootstarted'
[Install]
RequiredBy=coreos-installer.target`, bootStartedSignal)

var coreosInstallerMultipathUnit = `[Unit]
Description=TestISO Enable Multipath
Before=multipathd.service
DefaultDependencies=no
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/mpathconf --enable
[Install]
WantedBy=coreos-installer.target`

var waitForMpathTargetConf = `[Unit]
Requires=dev-mapper-mpatha.device
After=dev-mapper-mpatha.device`
