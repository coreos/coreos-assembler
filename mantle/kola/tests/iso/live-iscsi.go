package iso

import (
	_ "embed"
	"io"
	"os"
	"path/filepath"
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

var (
	tests_iscsi_x86_64 = []string{
		"iso-offline-install-iscsi.ibft.uefi",
		"iso-offline-install-iscsi.ibft-with-mpath.bios",
		"iso-offline-install-iscsi.manual.bios",
	}
	tests_iscsi_s390x = []string{
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.ibft.s390fw",
		//"iso-offline-install-iscsi.ibft-with-mpath.s390fw",
		//"iso-offline-install-iscsi.manual.s390fw",
	}
	tests_iscsi_ppc64le = []string{
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.ibft.ppcfw",
		//"iso-offline-install-iscsi.ibft-with-mpath.ppcfw",
		//"iso-offline-install-iscsi.manual.ppcfw",
	}
	tests_iscsi_aarch64 = []string{
		// FIXME https://github.com/coreos/fedora-coreos-tracker/issues/1657
		//"iso-offline-install-iscsi.ibft.uefi",
		//"iso-offline-install-iscsi.ibft-with-mpath.uefi",
		//"iso-offline-install-iscsi.manual.uefi",
	}
)

func getAllIscsiTests() []string {
	arch := coreosarch.CurrentRpmArch()
	switch arch {
	case "x86_64":
		return tests_iscsi_x86_64
	case "aarch64":
		return tests_iscsi_aarch64
	case "ppc64le":
		return tests_iscsi_ppc64le
	case "s390x":
		return tests_iscsi_s390x
	default:
		return []string{}
	}
}

func init() {
	for _, testName := range getAllIscsiTests() {
		register.RegisterTest(&register.Test{
			Run: func(c cluster.TestCluster) {
				opts := getIsoTestOpts(testName)
				testLiveSCSI(c, opts)
			},
			ClusterSize: 0,
			Name:        "iso." + testName,
			Description: "Verify iSCSI install works.",
			// Skip base checks (looks at journal for failures) until bootupd fix lands
			// https://github.com/coreos/fedora-coreos-tracker/issues/2136
			Tags:      []string{kola.NeedsInternetTag, kola.SkipBaseChecksTag},
			Timeout:   installTimeoutMins * time.Minute,
			Flags:     []register.Flag{},
			Platforms: []string{"qemu"},
		})
	}
}

//go:embed iscsi_butane_setup.yaml
var iscsi_butane_config string

// iscsi_butane_setup.yaml contains the full butane config but here is an overview of the setup
// 1 - Boot a live ISO with two extra 10G disks with labels "target" and "var"
//   - Format and mount `virtio-var` to /var
//
// 2 - target.container -> start an iscsi target, using quay.io/coreos-assembler/targetcli
// 3 - setup-targetcli.service calls /usr/local/bin/targetcli_script:
//   - instructs targetcli to serve /dev/disk/by-id/virtio-target as an iscsi target
//   - disables authentication
//   - verifies the iscsi service is active and reachable
//
// 4 - install-coreos-to-iscsi-target.service calls /usr/local/bin/install-coreos-iscsi:
//   - mount iscsi target
//   - run coreos-installer on the mounted block device
//   - unmount iscsi
//
// 5 - coreos-iscsi-vm.container -> start a coreos-assembler conainer:
//   - launch kola qemuexec instructing it to boot from an iPXE script
//     wich in turns mount the iscsi target and load kernel
//   - note the virtserial port device: we pass through the serial port
//     that was created by kola for test completion
//
// 6 - /var/nested-ign.json contains an ignition config:
//   - when the system is booted, write a success string to /dev/virtio-ports/testisocompletion
//   - as this serial device is mapped to the host serial device, the test concludes
func testLiveSCSI(c cluster.TestCluster, opts IsoTestOpts) {
	EnsureLiveArtifactsExist(c)

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}

	// Prepare config
	var butane string
	if opts.enableIbft && opts.enableMultipath {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1 --append-karg rd.multipath=default --append-karg root=/dev/disk/by-label/dm-mpath-root --append-karg rw")
	} else if opts.enableIbft {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1")
	} else if opts.manual {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg netroot=iscsi:10.0.2.15::::iqn.2024-05.com.coreos:0")
	}
	var iscsiTargetConfig = conf.Butane(butane)
	config, err := iscsiTargetConfig.Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	keys, err := qc.Keys()
	if err != nil {
		c.Fatal(err)
	}
	config.CopyKeys(keys)
	// Add a failure target to stop the test if something go wrong rather than waiting for the 10min timeout
	config.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	config.MountHost("/var/cosaroot", true)

	errchan := make(chan error)
	setupDisks := func(_ platform.MachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		if err := builder.AddIso(isopath, "", false); err != nil {
			return err
		}

		completionChannel, err := builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}
		go func() {
			errchan <- CheckTestOutput(completionChannel, []string{"iscsi-boot-ok"})
		}()

		// Create a serial channel to read the logs from the nested VM
		nestedVmLogsChannel, err := builder.VirtioChannelRead("nestedvmlogs")
		if err != nil {
			return err
		}
		// Create a file to write the contents of the serial channel into
		path := filepath.Join(filepath.Dir(builder.ConsoleFile), "nested_vm_console.txt")
		nestedVMConsole, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return err
		}
		go func() {
			_, err := io.Copy(nestedVMConsole, nestedVmLogsChannel)
			if err != nil && err != io.EOF {
				errchan <- errors.Wrap(err, "copying nested VM logs")
			}
		}()
		// empty disk to use as an iscsi target to install coreOS on and subseqently boot
		// Also add a 10G disk that we will mount on /var, to increase space available when pulling containers
		if err := builder.AddDisksFromSpecs([]string{"10G:serial=target", "10G:serial=var"}); err != nil {
			return err
		}
		// Bind mount in the COSA rootfs into the VM so we can use it as a
		// read-only rootfs for quickly starting the container to kola
		// qemuexec the nested VM for the test. See resources/iscsi_butane_setup.yaml
		builder.MountHost("/", "/var/cosaroot", true)
		return nil
	}

	// We need more memory to start another VM within !
	options := platform.MachineOptions{
		MinMemory: 2048,
		Firmware:  opts.firmware,
	}
	machineBuilder := &qemu.MachineBuilder{
		SetupDisks: setupDisks,
	}
	_, err = qc.NewMachineWithBuilder(config, options, machineBuilder)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	if err := <-errchan; err != nil {
		c.Fatal(err)
	}
}
