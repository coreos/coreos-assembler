package iso

import (
	_ "embed"
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
)

func init() {
	// Those currently work only on x86, see: https://github.com/coreos/fedora-coreos-tracker/issues/1657
	register.RegisterTest(isoTest("offline-install-iscsi.ibft.uefi", isoOfflineInstallIscsiIbftUefi, []string{"x86_64"}))
	register.RegisterTest(isoTest("offline-install-iscsi.ibft-with-mpath", isoOfflineInstallIscsiIbftMpath, []string{"x86_64"}))
	register.RegisterTest(isoTest("offline-install-iscsi.manual", isoOfflineInstallIscsiManual, []string{"x86_64"}))
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
	var outdir string
	//var qc *qemu.Cluster
	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		outdir = pc.RuntimeConf().OutputDir
		//qc = pc
	default:
		c.Fatalf("Unsupported cluster type")
	}

	var butane string
	if opts.enableIbft && opts.enableMultipath {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1 --append-karg rd.multipath=default --append-karg root=/dev/disk/by-label/dm-mpath-root --append-karg rw")
	} else if opts.enableIbft {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg rd.iscsi.firmware=1")
	} else if opts.manual {
		butane = strings.ReplaceAll(iscsi_butane_config, "COREOS_INSTALLER_KARGS", "--append-karg netroot=iscsi:10.0.2.15::::iqn.2024-05.com.coreos:0")
	}

	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		c.Fatalf("build %s is missing live artifacts", kola.CosaBuild.Meta.Name)
	}
	builddir := kola.CosaBuild.Dir
	isopath := filepath.Join(builddir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
	builder, err := newBaseQemuBuilder(opts, outdir)
	if err != nil {
		c.Fatal(err)
	}
	defer builder.Close()
	if err := builder.AddIso(isopath, "", false); err != nil {
		c.Fatal(err)
	}

	completionChannel, err := builder.VirtioChannelRead("testisocompletion")
	if err != nil {
		c.Fatal(err)
	}

	// Create a serial channel to read the logs from the nested VM
	nestedVmLogsChannel, err := builder.VirtioChannelRead("nestedvmlogs")
	if err != nil {
		c.Fatal(err)
	}

	// Create a file to write the contents of the serial channel into
	nestedVMConsole, err := os.OpenFile(filepath.Join(outdir, "nested_vm_console.txt"), os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		c.Fatal(err)
	}

	go func() {
		_, err := io.Copy(nestedVMConsole, nestedVmLogsChannel)
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()

	// empty disk to use as an iscsi target to install coreOS on and subseqently boot
	// Also add a 10G disk that we will mount on /var, to increase space available when pulling containers
	err = builder.AddDisksFromSpecs([]string{"10G:serial=target", "10G:serial=var"})
	if err != nil {
		c.Fatal(err)
	}

	// We need more memory to start another VM within !
	builder.MemoryMiB = 2048

	var iscsiTargetConfig = conf.Butane(butane)

	config, err := iscsiTargetConfig.Render(conf.FailWarnings)
	if err != nil {
		c.Fatal(err)
	}
	err = forwardJournal(outdir, builder, config)
	if err != nil {
		c.Fatal(err)
	}

	// Add a failure target to stop the test if something go wrong rather than waiting for the 10min timeout
	config.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)

	// enable network
	builder.EnableUsermodeNetworking([]platform.HostForwardPort{}, "")

	// keep auto-login enabled for easier debug when running console
	config.AddAutoLogin()

	builder.SetConfig(config)

	// Bind mount in the COSA rootfs into the VM so we can use it as a
	// read-only rootfs for quickly starting the container to kola
	// qemuexec the nested VM for the test. See resources/iscsi_butane_setup.yaml
	builder.MountHost("/", "/var/cosaroot", true)
	config.MountHost("/var/cosaroot", true)

	mach, err := builder.Exec()
	if err != nil {
		c.Fatal(err)
	}
	defer mach.Destroy()

	err = awaitCompletion(c, mach, opts.console, outdir, completionChannel, nil, []string{"iscsi-boot-ok"})
	if err != nil {
		c.Fatal(err)
	}
}
