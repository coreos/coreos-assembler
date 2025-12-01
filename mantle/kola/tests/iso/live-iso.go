package iso

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

func init() {
	register.RegisterTest(withInternet(isoTest("install", isoInstall, []string{"x86_64"})))

	register.RegisterTest(isoTest("offline-install", isoOfflineInstall, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.uefi", isoOfflineInstallUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("offline-install.4k", isoOfflineInstall4k, []string{"s390x"}))
	register.RegisterTest(isoTest("offline-install.mpath", isoOfflineInstallMpath, []string{"x86_64", "s390x", "ppc64le"}))
	register.RegisterTest(isoTest("offline-install.mpath.uefi", isoOfflineInstallMpathUefi, []string{"aarch64"}))
	register.RegisterTest(isoTest("offline-install-fromram.4k", isoOfflineInstallFromRam4k, []string{"ppc64le"}))
	register.RegisterTest(isoTest("offline-install-fromram.4k.uefi", isoOfflineInstallFromRam4kUefi, []string{"x86_64", "aarch64"}))

	register.RegisterTest(withInternet(isoTest("miniso-install", isoMinisoInstall, []string{"x86_64", "s390x", "ppc64le"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.uefi", isoMinisoInstallUefi, []string{"aarch64"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.4k", isoMinisoInstall4k, []string{"ppc64le"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.4k.uefi", isoMinisoInstall4kUefi, []string{"x86_64", "aarch64"})))

	register.RegisterTest(withInternet(isoTest("miniso-install.nm", isoMinisoInstallNm, []string{"x86_64", "s390x", "ppc64le"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.nm.uefi", isoMinisoInstallNmUefi, []string{"aarch64"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.4k.nm", isoMinisoInstall4kNm, []string{"ppc64le", "s390x"})))
	register.RegisterTest(withInternet(isoTest("miniso-install.4k.nm.uefi", isoMinisoInstall4kNmUefi, []string{"x86_64", "aarch64"})))
}

func isoMinisoInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isMiniso: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isMiniso:   true,
		enableUefi: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstall4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k: true,
		isMiniso: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstall4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:   true,
		enableUefi: true,
		isMiniso:   true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstallNm(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstallNmUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		isMiniso:     true,
		enableUefi:   true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstall4kNm(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		enable4k:     true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoMinisoInstall4kNmUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		addNmKeyfile: true,
		enable4k:     true,
		enableUefi:   true,
		isMiniso:     true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstall(c cluster.TestCluster) {
	opts := IsoTestOpts{
		isOffline: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstallUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableUefi: true,
		isOffline:  true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstall4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:  true,
		isOffline: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstallMpath(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableMultipath: true,
		isOffline:       true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstallMpathUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enableMultipath: true,
		enableUefi:      true,
		isOffline:       true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstallFromRam4k(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:     true,
		isOffline:    true,
		isISOFromRAM: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoOfflineInstallFromRam4kUefi(c cluster.TestCluster) {
	opts := IsoTestOpts{
		enable4k:     true,
		enableUefi:   true,
		isOffline:    true,
		isISOFromRAM: true,
	}
	opts.SetInsecureOnDevBuild()
	isoLiveInstall(c, opts)
}

func isoLiveInstall(c cluster.TestCluster, opts IsoTestOpts) {
	if err := ensureLiveArtifactsExist(); err != nil {
		fmt.Println(err)
		return
	}
	if opts.isMiniso && opts.isOffline { // ideally this'd be one enum parameter
		c.Fatal("Can't run minimal install offline")
	}
	if opts.isOffline && opts.addNmKeyfile {
		c.Fatal("Cannot use `--add-nm-keyfile` with offline mode")
	}

	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatalf("Unsupported cluster type")
	}
	if opts.enable4k {
		qc.EnforceNative4k()
	}
	if opts.enableMultipath {
		qc.EnforceMultipath()
	}

	tempdir, err := os.MkdirTemp("/var/tmp", "iso")
	if err != nil {
		c.Fatal(err)
	}
	defer func() {
		os.RemoveAll(tempdir)
	}()

	if err := isoRunTest(qc, opts, tempdir); err != nil {
		c.Fatal(err)
	}
}

func isoRunTest(qc *qemu.Cluster, opts IsoTestOpts, tempdir string) error {
	keys, err := qc.Keys()
	if err != nil {
		return err
	}
	targetConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return err
	}
	targetConfig.CopyKeys(keys)
	targetConfig.AddSystemdUnit("coreos-test-installer.service", signalCompletionUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	targetConfig.AddSystemdUnit("coreos-test-installer-no-ignition.service", checkNoIgnition, conf.Enable)
	if opts.enableMultipath {
		targetConfig.AddSystemdUnit("coreos-test-installer-multipathed.service", multipathedRoot, conf.Enable)
	}
	if opts.addNmKeyfile {
		targetConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
	}

	isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)

	installerConfig := CoreosInstallerConfig{
		IgnitionFile: "/var/opt/pointer.ign",
		DestDevice:   "/dev/vda",
		AppendKargs:  renderCosaTestIsoDebugKargs(),
		Insecure:     opts.instInsecure,
		CopyNetwork:  opts.addNmKeyfile, // force networking on in the initrd to verify the keyfile was used
	}

	var serializedTargetConfig string
	if opts.isOffline {
		// note we leave ImageURL empty here; offline installs should now be the
		// default!

		// we want to test that a full offline install works; that includes the
		// final installed host booting offline
		serializedTargetConfig = targetConfig.String()
	} else {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		baseurl := fmt.Sprintf("http://%s:%d", defaultQemuHostIPv4, port)

		// This is subtle but: for the minimal case, while we need networking to fetch the
		// rootfs, the primary install flow will still rely on osmet. So let's keep ImageURL
		// empty to exercise that path. In the future, this could be a separate scenario
		// (likely we should drop the "offline" naming and have a "remote" tag on the
		// opposite scenarios instead which fetch the metal image, so then we'd have
		// "[min]iso-install" and "[min]iso-remote-install").
		if opts.isMiniso {
			isopath, err = createMiniso(tempdir, isopath, baseurl)
			if err != nil {
				return err
			}
		} else {
			var metalimg string
			if opts.enable4k {
				metalimg = kola.CosaBuild.Meta.BuildArtifacts.Metal4KNative.Path
			} else {
				metalimg = kola.CosaBuild.Meta.BuildArtifacts.Metal.Path
			}
			metalname, err := setupMetalImage(kola.CosaBuild.Dir, metalimg, tempdir)
			if err != nil {
				return err
			}
			installerConfig.ImageURL = fmt.Sprintf("%s/%s", baseurl, metalname)
		}

		if opts.addNmKeyfile {
			nmKeyfiles := make(map[string]string)
			nmKeyfiles[nmConnectionFile] = nmConnection
			if err := embedNmkeyfiles(tempdir, nmKeyfiles, isopath); err != nil {
				return err
			}
		}

		// In this case; the target config is jut a tiny wrapper that wants to
		// fetch our hosted target.ign config
		// TODO also use https://github.com/coreos/coreos-installer/issues/118#issuecomment-585572952
		// when it arrives
		if err := targetConfig.WriteFile(filepath.Join(tempdir, "target.ign")); err != nil {
			return err
		}
		targetConfig, err = conf.EmptyIgnition().Render(conf.FailWarnings)
		if err != nil {
			return err
		}
		targetConfig.AddConfigSource(baseurl + "/target.ign")
		serializedTargetConfig = targetConfig.String()

		//nolint // Yeah this leaks
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/", http.FileServer(http.Dir(tempdir)))
			http.Serve(listener, mux)
		}()
	}

	// XXX: https://github.com/coreos/coreos-installer/issues/1171
	if coreosarch.CurrentRpmArch() != "s390x" {
		installerConfig.Console = []string{platform.ConsoleKernelArgument[coreosarch.CurrentRpmArch()]}
	}
	if opts.enableMultipath {
		// we only have one multipath device so it has to be that
		installerConfig.DestDevice = "/dev/mapper/mpatha"
		installerConfig.AppendKargs = append(installerConfig.AppendKargs, "rd.multipath=default", "root=/dev/disk/by-label/dm-mpath-root", "rw")
	}

	installerConfigData, err := yaml.Marshal(installerConfig)
	if err != nil {
		return err
	}
	mode := 0644

	liveConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return err
	}
	liveConfig.CopyKeys(keys)
	liveConfig.AddSystemdUnit("live-signal-ok.service", liveSignalOKUnit, conf.Enable)
	liveConfig.AddSystemdUnit("verify-no-efi-boot-entry.service", verifyNoEFIBootEntry, conf.Enable)
	liveConfig.AddSystemdUnit("iso-not-mounted-when-fromram.service", isoNotMountedUnit, conf.Enable)
	liveConfig.AddSystemdUnit("coreos-test-entered-emergency-target.service", signalFailureUnit, conf.Enable)
	volumeIdUnitContents := fmt.Sprintf(verifyIsoVolumeId, kola.CosaBuild.Meta.Name)
	liveConfig.AddSystemdUnit("verify-iso-volume-id.service", volumeIdUnitContents, conf.Enable)
	liveConfig.AddSystemdUnit("boot-started.service", bootStartedUnit, conf.Enable)
	liveConfig.AddFile(installerConfig.IgnitionFile, serializedTargetConfig, mode)
	liveConfig.AddFile("/etc/coreos/installer.d/mantle.yaml", string(installerConfigData), mode)
	liveConfig.AddAutoLogin()
	if opts.enableMultipath {
		liveConfig.AddSystemdUnit("coreos-installer-multipath.service", coreosInstallerMultipathUnit, conf.Enable)
		liveConfig.AddSystemdUnitDropin("coreos-installer.service", "wait-for-mpath-target.conf", waitForMpathTargetConf)
	}
	if opts.addNmKeyfile {
		liveConfig.AddSystemdUnit("coreos-test-nm-keyfile.service", verifyNmKeyfile, conf.Enable)
		// nmstate config via live Ignition config, propagated via
		// --copy-network, which is enabled by inst.NmKeyfiles
		liveConfig.AddFile(nmstateConfigFile, nmstateConfig, 0644)
	}

	overrideFW := func(builder *platform.QemuBuilder) error {
		builder.MemoryMiB = 4096
		if opts.enableUefi {
			builder.Firmware = "uefi"
		}
		kargs := renderCosaTestIsoDebugKargs()
		if opts.isISOFromRAM {
			// https://github.com/coreos/fedora-coreos-config/pull/2544
			kargs = append(kargs, "coreos.liveiso.fromram")
		}
		if opts.addNmKeyfile {
			kargs = append(kargs, "rd.neednet=1")
		}
		builder.AppendKernelArgs = strings.Join(kargs, " ")
		return nil
	}

	setupNet := func(o platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		if !opts.isOffline {
			// also save pointer config into the output dir for debugging
			path := filepath.Join(qc.RuntimeConf().OutputDir, builder.UUID, "config-target-pointer.ign")
			if err := targetConfig.WriteFile(path); err != nil {
				return err
			}
			return qc.SetupDefaultNetwork(o, builder)
		}
		return nil
	}

	var isoCompletionOutput *os.File
	var bootStartedOutput *os.File
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
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
				return err
			}
		} else {
			if err := builder.AddPrimaryDisk(&disk); err != nil {
				return err
			}
		}
		isoCompletionOutput, err = builder.VirtioChannelRead("testisocompletion")
		if err != nil {
			return errors.Wrap(err, "setting up testisocompletion virtio-serial channel")
		}
		bootStartedOutput, err = builder.VirtioChannelRead("bootstarted")
		if err != nil {
			return errors.Wrap(err, "setting up bootstarted virtio-serial channel")
		}
		return builder.AddIso(isopath, "bootindex=3", false)
	}

	extra := platform.QemuMachineOptions{}
	extra.SkipStartMachine = true
	callacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, SetupNetwork: setupNet, OverrideDefaults: overrideFW}
	qm, err := qc.NewMachineWithQemuOptionsAndBuilderCallbacks(liveConfig, extra, callacks)
	if err != nil {
		return errors.Wrap(err, "unable to create test machine")
	}

	errchan := make(chan error)
	go func() {
		errchan <- checkTestOutput(isoCompletionOutput, []string{liveOKSignal, signalCompleteString})
	}()

	//check for error when switching boot order
	go func() {
		if err := checkTestOutput(bootStartedOutput, []string{bootStartedSignal}); err != nil {
			errchan <- err
			return
		}
		if err := qc.Instance(qm).SwitchBootOrder(); err != nil {
			errchan <- errors.Wrapf(err, "switching boot order failed")
			return
		}
	}()

	err = <-errchan
	return err
}

func createMiniso(tempd string, isopath string, url string) (string, error) {
	minisopath := filepath.Join(tempd, "minimal.iso")
	// This is obviously also available in the build dir, but to be realistic,
	// let's take it from --rootfs-output
	rootfs_path := filepath.Join(tempd, "rootfs.img")
	// Ideally we'd use the coreos-installer of the target build here, because it's part
	// of the test workflow, but that's complex... Sadly, probably easiest is to spin up
	// a VM just to get the minimal ISO.
	cmd := exec.Command("coreos-installer", "iso", "extract", "minimal-iso", isopath,
		minisopath, "--output-rootfs", rootfs_path, "--rootfs-url", url+"/rootfs.img")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", errors.Wrapf(err, "running coreos-installer iso extract minimal")
	}
	return minisopath, nil
}

func embedNmkeyfiles(tempd string, nmKeyfiles map[string]string, isopath string) error {
	var keyfileArgs []string
	for nmName, nmContents := range nmKeyfiles {
		path := filepath.Join(tempd, nmName)
		if err := os.WriteFile(path, []byte(nmContents), 0600); err != nil {
			return err
		}
		keyfileArgs = append(keyfileArgs, "--keyfile", path)
	}
	if len(keyfileArgs) > 0 {
		args := []string{"iso", "network", "embed", isopath}
		args = append(args, keyfileArgs...)
		cmd := exec.Command("coreos-installer", args...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return errors.Wrapf(err, "running coreos-installer iso network embed")
		}
	}
	return nil
}
