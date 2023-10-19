// Copyright 2023 Red Hat
// Copyright 2015 CoreOS, Inc.
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/coreos/stream-metadata-go/stream"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/fcos"
	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/rhcos"
	"github.com/coreos/coreos-assembler/mantle/system"
	"github.com/coreos/coreos-assembler/mantle/util"
)

var (
	outputDir         string
	kolaPlatform      string
	kolaParallelArg   string
	kolaArchitectures = []string{"amd64"}
	kolaPlatforms     = []string{"aws", "azure", "do", "esx", "gcp", "openstack", "packet", "qemu", "qemu-iso"}
	kolaDistros       = []string{"fcos", "rhcos", "scos"}
)

func init() {
	sv := root.PersistentFlags().StringVar
	bv := root.PersistentFlags().BoolVar
	ss := root.PersistentFlags().StringSlice
	ssv := root.PersistentFlags().StringSliceVar

	// general options
	sv(&outputDir, "output-dir", "", "Temporary output directory for test data and logs")
	root.PersistentFlags().StringVarP(&kolaPlatform, "platform", "p", "", "VM platform: "+strings.Join(kolaPlatforms, ", "))
	root.PersistentFlags().StringVarP(&kola.Options.Distribution, "distro", "b", "", "Distribution: "+strings.Join(kolaDistros, ", "))
	root.PersistentFlags().StringVarP(&kolaParallelArg, "parallel", "j", "1", "number of tests to run in parallel, or \"auto\" to match CPU count")
	sv(&kola.TAPFile, "tapfile", "", "file to write TAP results to")
	root.PersistentFlags().BoolVarP(&kola.Options.UseWarnExitCode77, "on-warn-failure-exit-77", "", false, "Exit with code 77 if 'warn: true' tests fail")
	sv(&kola.Options.BaseName, "basename", "kola", "Cluster name prefix")
	ss("debug-systemd-unit", []string{}, "full-unit-name.service to enable SYSTEMD_LOG_LEVEL=debug on. Can be specified multiple times.")
	ssv(&kola.DenylistedTests, "denylist-test", []string{}, "Test pattern to add to denylist. Can be specified multiple times.")
	bv(&kola.NoNet, "no-net", false, "Don't run tests that require an Internet connection")
	bv(&kola.ForceRunPlatformIndependent, "run-platform-independent", false, "Run tests that claim platform independence")
	ssv(&kola.Tags, "tag", []string{}, "Test tag to run. Can be specified multiple times.")
	sv(&kola.Sharding, "sharding", "", "Provide e.g. 'hash:m/n' where m and n are integers, 1 <= m <= n.  Only tests hashing to m will be run.")
	bv(&kola.Options.SSHOnTestFailure, "ssh-on-test-failure", false, "SSH into a machine when tests fail")
	sv(&kola.Options.Stream, "stream", "", "CoreOS stream ID (e.g. for Fedora CoreOS: stable, testing, next)")
	sv(&kola.Options.CosaWorkdir, "workdir", "", "coreos-assembler working directory")
	sv(&kola.Options.CosaBuildId, "build", "", "coreos-assembler build ID")
	sv(&kola.Options.CosaBuildArch, "arch", coreosarch.CurrentRpmArch(), "The target architecture of the build")
	sv(&kola.Options.AppendButane, "append-butane", "", "Path to Butane config which is merged with test code")
	sv(&kola.Options.AppendIgnition, "append-ignition", "", "Path to Ignition config which is merged with test code")
	// we make this a percentage to avoid having to deal with floats
	root.PersistentFlags().UintVar(&kola.Options.ExtendTimeoutPercent, "extend-timeout-percentage", 0, "Extend all test timeouts by N percent")
	// rhcos-specific options
	sv(&kola.Options.OSContainer, "oscontainer", "", "oscontainer image pullspec for pivot (RHCOS only)")

	// aws-specific options
	defaultRegion := os.Getenv("AWS_REGION")
	if defaultRegion == "" {
		// As everyone knows, this is the one, true region.  Everything else is a mirage.
		defaultRegion = "us-east-1"
	}
	sv(&kola.AWSOptions.CredentialsFile, "aws-credentials-file", "", "AWS credentials file (default \"~/.aws/credentials\")")
	sv(&kola.AWSOptions.Region, "aws-region", defaultRegion, "AWS region")
	sv(&kola.AWSOptions.Profile, "aws-profile", "default", "AWS profile name")
	sv(&kola.AWSOptions.AMI, "aws-ami", "", `AWS AMI ID`)
	// See https://github.com/openshift/installer/issues/2919 for example
	sv(&kola.AWSOptions.InstanceType, "aws-type", "", "AWS instance type")
	sv(&kola.AWSOptions.SecurityGroup, "aws-sg", "kola", "AWS security group name")
	sv(&kola.AWSOptions.IAMInstanceProfile, "aws-iam-profile", "kola", "AWS IAM instance profile name")

	// azure-specific options
	sv(&kola.AzureOptions.AzureCredentials, "azure-credentials", "", "Azure credentials file location (default \"~/"+auth.AzureCredentialsPath+"\")")
	sv(&kola.AzureOptions.DiskURI, "azure-disk-uri", "", "Azure disk uri (custom images)")
	sv(&kola.AzureOptions.Publisher, "azure-publisher", "CoreOS", "Azure image publisher (default \"CoreOS\"")
	sv(&kola.AzureOptions.Offer, "azure-offer", "CoreOS", "Azure image offer (default \"CoreOS\"")
	sv(&kola.AzureOptions.Sku, "azure-sku", "alpha", "Azure image sku/channel (default \"alpha\"")
	sv(&kola.AzureOptions.Version, "azure-version", "", "Azure image version")
	sv(&kola.AzureOptions.Location, "azure-location", "westus", "Azure location (default \"westus\"")
	sv(&kola.AzureOptions.Size, "azure-size", "Standard_D2_v2", "Azure machine size (default \"Standard_D2_v2\")")

	// do-specific options
	sv(&kola.DOOptions.ConfigPath, "do-config-file", "", "DigitalOcean config file (default \"~/"+auth.DOConfigPath+"\")")
	sv(&kola.DOOptions.Profile, "do-profile", "", "DigitalOcean profile (default \"default\")")
	sv(&kola.DOOptions.AccessToken, "do-token", "", "DigitalOcean access token (overrides config file)")
	sv(&kola.DOOptions.Region, "do-region", "sfo2", "DigitalOcean region slug")
	sv(&kola.DOOptions.Size, "do-size", "1gb", "DigitalOcean size slug")
	sv(&kola.DOOptions.Image, "do-image", "alpha", "DigitalOcean image ID, {alpha, beta, stable}, or user image name")

	// esx-specific options
	sv(&kola.ESXOptions.ConfigPath, "esx-config-file", "", "ESX config file (default \"~/"+auth.ESXConfigPath+"\")")
	sv(&kola.ESXOptions.Server, "esx-server", "", "ESX server")
	sv(&kola.ESXOptions.Profile, "esx-profile", "", "ESX profile (default \"default\")")
	sv(&kola.ESXOptions.BaseVMName, "esx-base-vm", "", "ESX base VM name")

	// gcp-specific options
	sv(&kola.GCPOptions.Image, "gcp-image", "", "GCP image, full api endpoints names are accepted if resource is in a different project")
	sv(&kola.GCPOptions.Project, "gcp-project", "fedora-coreos-devel", "GCP project name")
	sv(&kola.GCPOptions.Zone, "gcp-zone", "us-central1-a", "GCP zone name")
	sv(&kola.GCPOptions.MachineType, "gcp-machinetype", "", "GCP machine type")
	sv(&kola.GCPOptions.DiskType, "gcp-disktype", "pd-ssd", "GCP disk type")
	sv(&kola.GCPOptions.Network, "gcp-network", "default", "GCP network")
	sv(&kola.GCPOptions.ServiceAcct, "gcp-service-account", "", "GCP service account to attach to instance (default project default)")
	bv(&kola.GCPOptions.ServiceAuth, "gcp-service-auth", false, "for non-interactive auth when running within GCP")
	sv(&kola.GCPOptions.JSONKeyFile, "gcp-json-key", "", "use a service account's JSON key for authentication (default \"~/"+auth.GCPConfigPath+"\")")
	bv(&kola.GCPOptions.Confidential, "gcp-confidential-vm", false, "create confidential instances")

	// openstack-specific options
	sv(&kola.OpenStackOptions.ConfigPath, "openstack-config-file", "", "Path to a clouds.yaml formatted OpenStack config file. The underlying library defaults to ./clouds.yaml")
	sv(&kola.OpenStackOptions.Profile, "openstack-profile", "", "OpenStack profile within clouds.yaml (default \"openstack\")")
	sv(&kola.OpenStackOptions.Region, "openstack-region", "", "OpenStack region")
	sv(&kola.OpenStackOptions.Image, "openstack-image", "", "OpenStack image ref")
	sv(&kola.OpenStackOptions.Flavor, "openstack-flavor", "1", "OpenStack flavor ref")
	sv(&kola.OpenStackOptions.Network, "openstack-network", "", "OpenStack network")
	sv(&kola.OpenStackOptions.Domain, "openstack-domain", "", "OpenStack domain ID")
	sv(&kola.OpenStackOptions.FloatingIPNetwork, "openstack-floating-ip-network", "", "OpenStack network to use when creating a floating IP")

	// packet-specific options
	sv(&kola.PacketOptions.ConfigPath, "packet-config-file", "", "Packet config file (default \"~/"+auth.PacketConfigPath+"\")")
	sv(&kola.PacketOptions.Profile, "packet-profile", "", "Packet profile (default \"default\")")
	sv(&kola.PacketOptions.ApiKey, "packet-api-key", "", "Packet API key (overrides config file)")
	sv(&kola.PacketOptions.Project, "packet-project", "", "Packet project UUID (overrides config file)")
	sv(&kola.PacketOptions.Facility, "packet-facility", "sjc1", "Packet facility code")
	sv(&kola.PacketOptions.Plan, "packet-plan", "", "Packet plan slug (default arch-dependent, e.g. \"t1.small.x86\")")
	sv(&kola.PacketOptions.Architecture, "packet-architecture", "x86_64", "Packet CPU architecture")
	sv(&kola.PacketOptions.IPXEURL, "packet-ipxe-url", "", "iPXE script URL (default arch-dependent, e.g. \"https://raw.githubusercontent.com/coreos/coreos-assembler/main/mantle/platform/api/packet/fcos-x86_64.ipxe\")")
	sv(&kola.PacketOptions.ImageURL, "packet-image-url", "", "image URL (default arch-dependent, e.g. \"https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/31.20200223.3.0/x86_64/fedora-coreos-31.20200223.3.0-metal.x86_64.raw.xz\")")

	// QEMU-specific options
	sv(&kola.QEMUOptions.Firmware, "qemu-firmware", "", "Boot firmware: bios,uefi,uefi-secure (default bios)")
	sv(&kola.QEMUOptions.DiskImage, "qemu-image", "", "path to CoreOS disk image")
	sv(&kola.QEMUOptions.DiskSize, "qemu-size", "", "Resize target disk via qemu-img resize [+]SIZE")
	sv(&kola.QEMUOptions.DriveOpts, "qemu-drive-opts", "", "Arbitrary options to append to qemu -drive for primary disk")
	sv(&kola.QEMUOptions.Memory, "qemu-memory", "", "Default memory size in MB")
	bv(&kola.QEMUOptions.NbdDisk, "qemu-nbd-socket", false, "Present the disks over NBD socket to qemu")
	bv(&kola.QEMUOptions.MultiPathDisk, "qemu-multipath", false, "Enable multiple paths for the main disk")
	bv(&kola.QEMUOptions.Native4k, "qemu-native-4k", false, "Force 4k sectors for main disk")
	bv(&kola.QEMUOptions.Nvme, "qemu-nvme", false, "Use NVMe for main disk")
	bv(&kola.QEMUOptions.Swtpm, "qemu-swtpm", true, "Create temporary software TPM")
	ssv(&kola.QEMUOptions.BindRO, "qemu-bind-ro", nil, "Inject a host directory; this does not automatically mount in the guest")

	sv(&kola.QEMUIsoOptions.IsoPath, "qemu-iso", "", "path to CoreOS ISO image")
	bv(&kola.QEMUIsoOptions.AsDisk, "qemu-iso-as-disk", false, "attach ISO image as regular disk")
	// s390x secex specific options
	bv(&kola.QEMUOptions.SecureExecution, "qemu-secex", false, "Run IBM Secure Execution Image")
	sv(&kola.QEMUOptions.SecureExecutionIgnitionPubKey, "qemu-secex-ignition-pubkey", "", "Path to Ignition GPG Public Key")
	sv(&kola.QEMUOptions.SecureExecutionHostKey, "qemu-secex-hostkey", "", "Path to Secure Execution HKD certificate")
}

// Sync up the command line options if there is dependency
func syncOptionsImpl(useCosa bool) error {
	validateOption := func(name, item string, valid []string) error {
		for _, v := range valid {
			if v == item {
				return nil
			}
		}
		return fmt.Errorf("unsupported %v %q", name, item)
	}

	if kolaPlatform == "iso" {
		kolaPlatform = "qemu-iso"
	}

	if kolaPlatform == "" && kola.QEMUIsoOptions.IsoPath != "" {
		kolaPlatform = "qemu-iso"
	}

	// There used to be two QEMU platforms: privileged ('qemu') and
	// unprivileged ('qemu-unpriv'). We first removed support for privileged
	// QEMU and aliased it to 'qemu-unpriv' and then renamed and merged
	// 'qemu-unpriv' to 'qemu' to unify on a single name. 'qemu' is now the
	// default.
	if kolaPlatform == "" {
		kolaPlatform = "qemu"
	}

	// test parallelism
	if kolaParallelArg == "auto" {
		ncpu, err := system.GetProcessors()
		if err != nil {
			return fmt.Errorf("detecting CPU count: %w", err)
		}
		kola.TestParallelism = int(ncpu)
	} else {
		parallel, err := strconv.ParseInt(kolaParallelArg, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing --parallel argument: %w", err)
		}
		kola.TestParallelism = int(parallel)
	}

	// native 4k requires a UEFI bootloader
	if kola.QEMUOptions.Native4k && kola.QEMUOptions.Firmware == "bios" {
		return fmt.Errorf("native 4k requires uefi firmware")
	}
	// default to BIOS, UEFI for aarch64 and x86(only for 4k)
	if kola.QEMUOptions.Firmware == "" {
		if kola.Options.CosaBuildArch == "aarch64" {
			kola.QEMUOptions.Firmware = "uefi"
		} else if kola.Options.CosaBuildArch == "x86_64" && kola.QEMUOptions.Native4k {
			kola.QEMUOptions.Firmware = "uefi"
		}
	}

	if err := validateOption("platform", kolaPlatform, kolaPlatforms); err != nil {
		return err
	}

	// Choose an appropriate AWS instance type for the target architecture
	if kolaPlatform == "aws" && kola.AWSOptions.InstanceType == "" {
		switch kola.Options.CosaBuildArch {
		case "x86_64":
			kola.AWSOptions.InstanceType = "m5.large"
		case "aarch64":
			kola.AWSOptions.InstanceType = "c6g.xlarge"
		}
		fmt.Printf("Using %s instance type\n", kola.AWSOptions.InstanceType)
	}

	// Choose an appropriate GCP instance type for the target architecture
	if kolaPlatform == "gcp" && kola.GCPOptions.MachineType == "" {
		switch kola.Options.CosaBuildArch {
		case "x86_64":
			if kola.GCPOptions.Confidential {
				// https://cloud.google.com/compute/confidential-vm/docs/locations
				fmt.Print("Setting instance type for confidential computing")
				kola.GCPOptions.MachineType = "n2d-standard-2"
			} else {
				kola.GCPOptions.MachineType = "n1-standard-1"
			}
		case "aarch64":
			kola.GCPOptions.MachineType = "t2a-standard-1"
		}
		fmt.Printf("Using %s instance type\n", kola.GCPOptions.MachineType)
	}

	// if no external dirs were given, automatically add the working directory;
	// does nothing if ./tests/kola/ doesn't exist
	if len(runExternals) == 0 {
		runExternals = []string{"."}
	}

	foundCosa := false
	if kola.Options.CosaBuildId != "" {
		// specified --build? fetch that build. in this path we *require* a
		// cosa workdir, either assumed as PWD or via --workdir.

		if kola.Options.CosaWorkdir == "" {
			kola.Options.CosaWorkdir = "."
		}

		localbuild, err := util.GetLocalBuild(kola.Options.CosaWorkdir,
			kola.Options.CosaBuildId,
			kola.Options.CosaBuildArch)
		if err != nil {
			return err
		}

		kola.CosaBuild = localbuild
		foundCosa = true
	} else if kola.Options.Stream != "" {
		if err := syncStreamOptions(); err != nil {
			return err
		}
	} else {
		if kola.Options.CosaWorkdir == "" {
			// specified neither --build nor --workdir; only opportunistically
			// try to use the PWD as the workdir, but don't error out if it's
			// not
			if isroot, err := util.IsCosaRoot("."); err != nil {
				return err
			} else if isroot {
				kola.Options.CosaWorkdir = "."
			}
		}

		if kola.Options.CosaWorkdir != "" && kola.Options.CosaWorkdir != "none" {
			localbuild, err := util.GetLatestLocalBuild(kola.Options.CosaWorkdir,
				kola.Options.CosaBuildArch)
			if err != nil {
				if !os.IsNotExist(errors.Cause(err)) {
					return err
				}
			} else {
				kola.Options.CosaBuildId = localbuild.Meta.BuildID
				kola.CosaBuild = localbuild
				foundCosa = true
			}
		} else if kola.QEMUOptions.DiskImage == "" {
			localbuild, err := util.GetLocalFastBuildQemu()
			if err != nil {
				return err
			}
			if localbuild != "" {
				kola.QEMUOptions.DiskImage = localbuild
			}
		}
	}

	if foundCosa && useCosa {
		if err := syncCosaOptions(); err != nil {
			return err
		}
	}
	// Currently the `--arch` option is defined in terms of coreos-assembler, but
	// we also unconditionally use it for qemu if present.
	kola.QEMUOptions.Arch = kola.Options.CosaBuildArch

	units, _ := root.PersistentFlags().GetStringSlice("debug-systemd-units")
	for _, unit := range units {
		kola.Options.SystemdDropins = append(kola.Options.SystemdDropins, platform.SystemdDropin{
			Unit:     unit,
			Name:     "10-debug.conf",
			Contents: "[Service]\nEnvironment=SYSTEMD_LOG_LEVEL=debug",
		})
	}

	if kola.Options.Distribution == "" {
		kola.Options.Distribution = kolaDistros[0]
	} else if kola.Options.Distribution == "scos" {
		// Consider SCOS the same as RHCOS for now
		kola.Options.Distribution = "rhcos"
	} else if err := validateOption("distro", kola.Options.Distribution, kolaDistros); err != nil {
		return err
	}

	return nil
}

// syncOptions updates default values of options based on provided ones
func syncOptions() error {
	return syncOptionsImpl(true)
}

// syncCosaOptions sets unset platform-specific
// options that can be derived from the cosa build metadata
func syncCosaOptions() error {
	switch kolaPlatform {
	case "qemu":
		if kola.QEMUOptions.SecureExecution && kola.QEMUOptions.DiskImage == "" && kola.CosaBuild.Meta.BuildArtifacts.SecureExecutionQemu != nil {
			kola.QEMUOptions.DiskImage = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.SecureExecutionQemu.Path)
		}
		if kola.QEMUOptions.SecureExecutionIgnitionPubKey == "" && kola.CosaBuild.Meta.BuildArtifacts.SecureExecutionIgnitionPubKey != nil {
			kola.QEMUOptions.SecureExecutionIgnitionPubKey = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.SecureExecutionIgnitionPubKey.Path)
		}
		if kola.QEMUOptions.DiskImage == "" && kola.CosaBuild.Meta.BuildArtifacts.Qemu != nil {
			kola.QEMUOptions.DiskImage = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.Qemu.Path)
		}
	case "qemu-iso":
		if kola.QEMUIsoOptions.IsoPath == "" && kola.CosaBuild.Meta.BuildArtifacts.LiveIso != nil {
			kola.QEMUIsoOptions.IsoPath = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		}
	case "aws":
		// Pick up the AMI from the build metadata
		if kola.AWSOptions.AMI == "" {
			for _, ami := range kola.CosaBuild.Meta.Amis {
				if ami.Region == kola.AWSOptions.Region {
					kola.AWSOptions.AMI = ami.Hvm
					fmt.Printf("Using AMI %s from Region %s\n", kola.AWSOptions.AMI, kola.AWSOptions.Region)
				}
			}
		}
	case "gcp":
		// Pick up the GCP image from the build metadata
		if kola.GCPOptions.Image == "" && kola.CosaBuild.Meta.Gcp != nil {
			kola.GCPOptions.Image =
				fmt.Sprintf("projects/%s/global/images/%s",
					kola.CosaBuild.Meta.Gcp.ImageProject,
					kola.CosaBuild.Meta.Gcp.ImageName)

			fmt.Printf("Using GCP image %s\n", kola.GCPOptions.Image)
		}
	}

	if kola.Options.Distribution == "" {
		distro, err := util.TargetDistro(kola.CosaBuild.Meta)
		if err != nil {
			return err
		}
		kola.Options.Distribution = distro
	}

	runExternals = append(runExternals, filepath.Join(kola.Options.CosaWorkdir, "src/config"))

	return nil
}

// syncStreamOptions sets the underlying raw options based on a stream
// Currently this only handles AWS to demonstrate the idea; we'll
// add generic code to map between streams and cosa builds soon.
func syncStreamOptions() error {
	if kola.Options.Stream == "" {
		return nil
	}
	var err error
	var artifacts *stream.Arch
	switch kola.Options.Distribution {
	case "":
		return fmt.Errorf("Must specify -b/--distro with --stream")
	case "fcos":
		artifacts, err = fcos.FetchCanonicalStreamArtifacts(kola.Options.Stream, kola.Options.CosaBuildArch)
		if err != nil {
			return errors.Wrapf(err, "failed to fetch stream")
		}
	case "rhcos":
		artifacts, err = rhcos.FetchStreamArtifacts(kola.Options.Stream, kola.Options.CosaBuildArch)
		if err != nil {
			return errors.Wrapf(err, "failed to fetch stream")
		}
	default:
		return fmt.Errorf("Unhandled stream for distribution %s", kola.Options.Distribution)
	}

	release := ""
	extra := ""

	switch kolaPlatform {
	case "aws":
		regionimg := artifacts.Images.Aws.Regions[kola.AWSOptions.Region]
		release = regionimg.Release
		kola.AWSOptions.AMI = regionimg.Image
		extra = fmt.Sprintf("(region %s, %s)", kola.AWSOptions.Region, kola.AWSOptions.AMI)
	default:
		return fmt.Errorf("Unhandled platform %s for stream", kolaPlatform)
	}

	fmt.Printf("Resolved distro=%s stream=%s platform=%s arch=%s to release=%s %s\n",
		kola.Options.Distribution, kola.Options.Stream,
		kolaPlatform, kola.Options.CosaBuildArch, release, extra)

	return nil
}
