package iso

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/pkg/errors"
)

const (
	installTimeoutMins = 12

	// defaultQemuHostIPv4 is documented in `man qemu-kvm`, under the `-netdev` option
	defaultQemuHostIPv4 = "10.0.2.2"
)

// This object gets serialized to YAML and fed to coreos-installer:
// https://coreos.github.io/coreos-installer/customizing-install/#config-file-format
type CoreosInstallerConfig struct {
	ImageURL     string   `yaml:"image-url,omitempty"`
	IgnitionFile string   `yaml:"ignition-file,omitempty"`
	Insecure     bool     `yaml:"insecure,omitempty"`
	AppendKargs  []string `yaml:"append-karg,omitempty"`
	CopyNetwork  bool     `yaml:"copy-network,omitempty"`
	DestDevice   string   `yaml:"dest-device,omitempty"`
	Console      []string `yaml:"console,omitempty"`
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

func checkTestOutput(output *os.File, expected []string) error {
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

func ensureLiveArtifactsExist() error {
	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil {
		return errors.Errorf("Build %s is missing live-iso artifacts\n", kola.CosaBuild.Meta.Name)
	}
	if kola.CosaBuild.Meta.BuildArtifacts.LiveRootfs == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveInitramfs == nil {
		return errors.Errorf("Build %s is missing live artifacts\n", kola.CosaBuild.Meta.Name)
	}
	if kola.CosaBuild.Meta.BuildArtifacts.Metal == nil || kola.CosaBuild.Meta.BuildArtifacts.Metal4KNative == nil {
		return errors.Errorf("Build %s is missing live metal artifacts\n", kola.CosaBuild.Meta.Name)
	}
	return nil
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

// setupMetalImage creates a symlink to the metal image.
func setupMetalImage(builddir, metalimg, destdir string) (string, error) {
	if err := absSymlink(filepath.Join(builddir, metalimg), filepath.Join(destdir, metalimg)); err != nil {
		return "", err
	}
	return metalimg, nil
}

func cat(outfile string, infiles ...string) error {
	out, err := os.OpenFile(outfile, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, infile := range infiles {
		in, err := os.Open(infile)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(out, in)
		if err != nil {
			return err
		}
	}
	return nil
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
