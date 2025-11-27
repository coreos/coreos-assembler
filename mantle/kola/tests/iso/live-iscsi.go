package iso

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/pkg/errors"
)

// Don't restrict networking for iSCSI tests; otherwise they fail.
func withInternet(t *register.Test) *register.Test {
	t.Tags = append(t.Tags, "needs-internet")
	return t
}

func init() {
	// Those currently work only on x86, see: https://github.com/coreos/fedora-coreos-tracker/issues/1657
	register.RegisterTest(withInternet(isoTest("offline-install-iscsi.ibft.uefi", isoOfflineInstallIscsiIbftUefi, []string{"x86_64"})))
	register.RegisterTest(withInternet(isoTest("offline-install-iscsi.ibft-with-mpath", isoOfflineInstallIscsiIbftMpath, []string{"x86_64"})))
	register.RegisterTest(withInternet(isoTest("offline-install-iscsi.manual", isoOfflineInstallIscsiManual, []string{"x86_64"})))
}

func isoOfflineInstallIscsiIbftUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
		isOffline:  true,
		enableIbft: true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
}

func isoOfflineInstallIscsiIbftMpath(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi:      true,
		isOffline:       true,
		enableMultipath: true,
		enableIbft:      true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
}

func isoOfflineInstallIscsiManual(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline:  true,
		manual:     true,
		enableIbft: true,
	}
	opts.SetInsecureOnDevBuild()
	isoInstalliScsi(c, opts)
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
func isoInstalliScsi(c cluster.TestCluster, opts IsoTestOpts) {
	if err := EnsureLiveArtifactsExist(); err != nil {
		fmt.Println(err)
		return
	}

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

	overrideFW := func(builder *platform.QemuBuilder) error {
		if opts.enableUefi {
			builder.Firmware = "uefi"
		}
		// We need more memory to start another VM within !
		builder.MemoryMiB = 2048
		return nil
	}

	errchan := make(chan error)
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		if err := builder.AddIso(isopath, "", false); err != nil {
			return err
		}

		output, err := builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

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
				fmt.Printf("error copying nested VM logs: %v\n", err)
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

		// Read line in a goroutine and send errors to channel
		go func() {
			errchan <- CheckTestOutput(output, []string{"iscsi-boot-ok"})
		}()

		return nil
	}

	callacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
	_, err = qc.NewMachineWithQemuOptionsAndBuilderCallbacks(config, platform.QemuMachineOptions{}, callacks)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	err = <-errchan
	if err != nil {
		c.Fatal(err)
	}
}
