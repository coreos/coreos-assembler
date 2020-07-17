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
	"strings"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/system"
)

var (
	outputDir                   string
	kolaPlatform                string
	kolaArchitectures           = []string{"amd64"}
	kolaPlatforms               = []string{"aws", "azure", "do", "esx", "gce", "openstack", "packet", "qemu", "qemu-unpriv", "qemu-iso"}
	kolaDistros                 = []string{"fcos", "rhcos"}
	kolaIgnitionVersionDefaults = map[string]string{
		"cl":    "v2",
		"fcos":  "v3",
		"rhcos": "v3",
	}
)

func init() {
	sv := root.PersistentFlags().StringVar
	bv := root.PersistentFlags().BoolVar
	ss := root.PersistentFlags().StringSlice
	ssv := root.PersistentFlags().StringSliceVar

	// general options
	sv(&outputDir, "output-dir", "", "Temporary output directory for test data and logs")
	root.PersistentFlags().StringVarP(&kolaPlatform, "platform", "p", "qemu-unpriv", "VM platform: "+strings.Join(kolaPlatforms, ", "))
	root.PersistentFlags().StringVarP(&kola.Options.Distribution, "distro", "b", "", "Distribution: "+strings.Join(kolaDistros, ", "))
	root.PersistentFlags().IntVarP(&kola.TestParallelism, "parallel", "j", 1, "number of tests to run in parallel")
	sv(&kola.TAPFile, "tapfile", "", "file to write TAP results to")
	root.PersistentFlags().BoolVarP(&kola.Options.NoTestExitError, "no-test-exit-error", "T", false, "Don't exit with non-zero if tests fail")
	sv(&kola.Options.BaseName, "basename", "kola", "Cluster name prefix")
	ss("debug-systemd-unit", []string{}, "full-unit-name.service to enable SYSTEMD_LOG_LEVEL=debug on. Can be specified multiple times.")
	sv(&kola.Options.IgnitionVersion, "ignition-version", "", "Ignition version override: v2, v3")
	ssv(&kola.DenylistedTests, "denylist-test", []string{}, "Test pattern to add to denylist. Can be specified multiple times.")
	bv(&kola.NoNet, "no-net", false, "Don't run tests that require an Internet connection")
	ssv(&kola.Tags, "tag", []string{}, "Test tag to run. Can be specified multiple times.")
	bv(&kola.Options.SSHOnTestFailure, "ssh-on-test-failure", false, "SSH into a machine when tests fail")
	sv(&kola.Options.CosaWorkdir, "workdir", "", "coreos-assembler working directory")
	sv(&kola.Options.CosaBuildId, "build", "", "coreos-assembler build ID")
	// rhcos-specific options
	sv(&kola.Options.OSContainer, "oscontainer", "", "oscontainer image pullspec for pivot (RHCOS only)")

	// aws-specific options
	defaultRegion := os.Getenv("AWS_REGION")
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	sv(&kola.AWSOptions.CredentialsFile, "aws-credentials-file", "", "AWS credentials file (default \"~/.aws/credentials\")")
	sv(&kola.AWSOptions.Region, "aws-region", defaultRegion, "AWS region")
	sv(&kola.AWSOptions.Profile, "aws-profile", "default", "AWS profile name")
	sv(&kola.AWSOptions.AMI, "aws-ami", "alpha", `AWS AMI ID, or (alpha|beta|stable) to use the latest image`)
	sv(&kola.AWSOptions.InstanceType, "aws-type", "m4.large", "AWS instance type")
	sv(&kola.AWSOptions.SecurityGroup, "aws-sg", "kola", "AWS security group name")
	sv(&kola.AWSOptions.IAMInstanceProfile, "aws-iam-profile", "kola", "AWS IAM instance profile name")

	// azure-specific options
	sv(&kola.AzureOptions.AzureProfile, "azure-profile", "", "Azure profile (default \"~/"+auth.AzureProfilePath+"\")")
	sv(&kola.AzureOptions.AzureAuthLocation, "azure-auth", "", "Azure auth location (default \"~/"+auth.AzureAuthPath+"\")")
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

	// gce-specific options
	sv(&kola.GCEOptions.Image, "gce-image", "projects/coreos-cloud/global/images/family/coreos-alpha", "GCE image, full api endpoints names are accepted if resource is in a different project")
	sv(&kola.GCEOptions.Project, "gce-project", "coreos-gce-testing", "GCE project name")
	sv(&kola.GCEOptions.Zone, "gce-zone", "us-central1-a", "GCE zone name")
	sv(&kola.GCEOptions.MachineType, "gce-machinetype", "n1-standard-1", "GCE machine type")
	sv(&kola.GCEOptions.DiskType, "gce-disktype", "pd-ssd", "GCE disk type")
	sv(&kola.GCEOptions.Network, "gce-network", "default", "GCE network")
	bv(&kola.GCEOptions.ServiceAuth, "gce-service-auth", false, "for non-interactive auth when running within GCE")
	sv(&kola.GCEOptions.JSONKeyFile, "gce-json-key", "", "use a service account's JSON key for authentication")

	// openstack-specific options
	sv(&kola.OpenStackOptions.ConfigPath, "openstack-config-file", "", "OpenStack config file (default \"~/"+auth.OpenStackConfigPath+"\")")
	sv(&kola.OpenStackOptions.Profile, "openstack-profile", "", "OpenStack profile (default \"default\")")
	sv(&kola.OpenStackOptions.Region, "openstack-region", "", "OpenStack region")
	sv(&kola.OpenStackOptions.Image, "openstack-image", "", "OpenStack image ref")
	sv(&kola.OpenStackOptions.Flavor, "openstack-flavor", "1", "OpenStack flavor ref")
	sv(&kola.OpenStackOptions.Network, "openstack-network", "", "OpenStack network")
	sv(&kola.OpenStackOptions.Domain, "openstack-domain", "", "OpenStack domain ID")
	sv(&kola.OpenStackOptions.FloatingIPPool, "openstack-floating-ip-pool", "", "OpenStack floating IP pool for Compute v2 networking")

	// packet-specific options
	sv(&kola.PacketOptions.ConfigPath, "packet-config-file", "", "Packet config file (default \"~/"+auth.PacketConfigPath+"\")")
	sv(&kola.PacketOptions.Profile, "packet-profile", "", "Packet profile (default \"default\")")
	sv(&kola.PacketOptions.ApiKey, "packet-api-key", "", "Packet API key (overrides config file)")
	sv(&kola.PacketOptions.Project, "packet-project", "", "Packet project UUID (overrides config file)")
	sv(&kola.PacketOptions.Facility, "packet-facility", "sjc1", "Packet facility code")
	sv(&kola.PacketOptions.Plan, "packet-plan", "", "Packet plan slug (default arch-dependent, e.g. \"t1.small.x86\")")
	sv(&kola.PacketOptions.Architecture, "packet-architecture", "x86_64", "Packet CPU architecture")
	sv(&kola.PacketOptions.IPXEURL, "packet-ipxe-url", "", "iPXE script URL (default arch-dependent, e.g. \"https://raw.githubusercontent.com/coreos/coreos-assembler/master/mantle/platform/api/packet/fcos-x86_64.ipxe\")")
	sv(&kola.PacketOptions.ImageURL, "packet-image-url", "", "image URL (default arch-dependent, e.g. \"https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/31.20200223.3.0/x86_64/fedora-coreos-31.20200223.3.0-metal.x86_64.raw.xz\")")

	// QEMU-specific options
	sv(&kola.QEMUOptions.Firmware, "qemu-firmware", "", "Boot firmware: bios,uefi,uefi-secure (default bios)")
	sv(&kola.QEMUOptions.DiskImage, "qemu-image", "", "path to CoreOS disk image")
	sv(&kola.QEMUOptions.DiskSize, "qemu-size", "", "Resize target disk via qemu-img resize [+]SIZE")
	sv(&kola.QEMUOptions.Memory, "qemu-memory", "", "Default memory size in MB")
	bv(&kola.QEMUOptions.NbdDisk, "qemu-nbd-socket", false, "Present the disks over NBD socket to qemu")
	bv(&kola.QEMUOptions.MultiPathDisk, "qemu-multipath", false, "Enable multiple paths for the main disk")
	bv(&kola.QEMUOptions.Native4k, "qemu-native-4k", false, "Force 4k sectors for main disk")
	bv(&kola.QEMUOptions.Nvme, "qemu-nvme", false, "Use NVMe for main disk")
	bv(&kola.QEMUOptions.Swtpm, "qemu-swtpm", true, "Create temporary software TPM")

	sv(&kola.QEMUIsoOptions.IsoPath, "qemu-iso", "", "path to CoreOS ISO image")
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

	// There used to be a "privileged" qemu path, it is no longer supported.
	// Alias qemu to qemu-unpriv.
	if kolaPlatform == "qemu" {
		kolaPlatform = "qemu-unpriv"
	}

	// native 4k requires a UEFI bootloader
	if kola.QEMUOptions.Native4k && kola.QEMUOptions.Firmware == "bios" {
		return fmt.Errorf("native 4k requires uefi firmware")
	}
	// default to BIOS, UEFI for aarch64 and x86(only for 4k)
	if kola.QEMUOptions.Firmware == "" {
		if system.RpmArch() == "aarch64" {
			kola.QEMUOptions.Firmware = "uefi"
		} else if system.RpmArch() == "x86_64" && kola.QEMUOptions.Native4k {
			kola.QEMUOptions.Firmware = "uefi"
		} else {
			kola.QEMUOptions.Firmware = "bios"
		}
	}

	if err := validateOption("platform", kolaPlatform, kolaPlatforms); err != nil {
		return err
	}

	foundCosa := false
	if kola.Options.CosaBuildId != "" {
		// specified --build? fetch that build. in this path we *require* a
		// cosa workdir, either assumed as PWD or via --workdir.

		if kola.Options.CosaWorkdir == "" {
			kola.Options.CosaWorkdir = "."
		}

		localbuild, err := sdk.GetLocalBuild(kola.Options.CosaWorkdir, kola.Options.CosaBuildId)
		if err != nil {
			return err
		}

		kola.CosaBuild = localbuild
		foundCosa = true
	} else {
		if kola.Options.CosaWorkdir == "" {
			// specified neither --build nor --workdir; only opportunistically
			// try to use the PWD as the workdir, but don't error out if it's
			// not
			if isroot, err := sdk.IsCosaRoot("."); err != nil {
				return err
			} else if isroot {
				kola.Options.CosaWorkdir = "."
			}
		}

		if kola.Options.CosaWorkdir != "" && kola.Options.CosaWorkdir != "none" {
			localbuild, err := sdk.GetLatestLocalBuild(kola.Options.CosaWorkdir)
			if err != nil {
				return err
			}

			kola.Options.CosaBuildId = localbuild.Meta.BuildID
			kola.CosaBuild = localbuild
			foundCosa = true
		}
	}

	if foundCosa && useCosa {
		if err := syncCosaOptions(); err != nil {
			return err
		}
	}

	if kola.Options.IgnitionVersion == "" && kola.QEMUOptions.DiskImage != "" {
		kola.Options.IgnitionVersion = sdk.TargetIgnitionVersionFromName(kola.QEMUOptions.DiskImage)
	}

	units, _ := root.PersistentFlags().GetStringSlice("debug-systemd-units")
	for _, unit := range units {
		kola.Options.SystemdDropins = append(kola.Options.SystemdDropins, platform.SystemdDropin{
			Unit:     unit,
			Name:     "10-debug.conf",
			Contents: "[Service]\nEnvironment=SYSTEMD_LOG_LEVEL=debug",
		})
	}

	if kola.Options.OSContainer != "" && kola.Options.Distribution != "rhcos" {
		return fmt.Errorf("oscontainer is only supported on rhcos")
	}

	if kola.Options.Distribution == "" {
		kola.Options.Distribution = kolaDistros[0]
	} else if err := validateOption("distro", kola.Options.Distribution, kolaDistros); err != nil {
		return err
	}

	if kola.Options.IgnitionVersion == "" {
		var ok bool
		kola.Options.IgnitionVersion, ok = kolaIgnitionVersionDefaults[kola.Options.Distribution]
		if !ok {
			return fmt.Errorf("Distribution %q has no default Ignition version", kola.Options.Distribution)
		}
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
	case "qemu-unpriv", "qemu":
		if kola.QEMUOptions.DiskImage == "" && kola.CosaBuild.Meta.BuildArtifacts.Qemu != nil {
			kola.QEMUOptions.DiskImage = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.Qemu.Path)
		}
	case "qemu-iso":
		if kola.QEMUIsoOptions.IsoPath == "" && kola.CosaBuild.Meta.BuildArtifacts.LiveIso != nil {
			kola.QEMUIsoOptions.IsoPath = filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		}
	}

	if kola.Options.IgnitionVersion == "" && kola.QEMUOptions.DiskImage == "" {
		if kola.CosaBuild != nil {
			kola.Options.IgnitionVersion = sdk.TargetIgnitionVersion(kola.CosaBuild.Meta)
		}
	}

	if kola.Options.Distribution == "" {
		distro, err := sdk.TargetDistro(kola.CosaBuild.Meta)
		if err != nil {
			return err
		}
		kola.Options.Distribution = distro
	}

	runExternals = append(runExternals, filepath.Join(kola.Options.CosaWorkdir, "src/config"))

	return nil
}
