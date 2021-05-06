# Mantle platforms

Platforms are an API interface to different environments to run clusters,
create images, collect logging information, etc.

## Authentication

Authentication differs based on the platform. Some platforms like `aws` utilize the
configuration files from their command-line tooling while others define their
own custom configuration format and default locations (like [DigitalOcean](https://github.com/coreos/mantle/tree/master/auth/do.go)).
Generally if any extensions / custom configurations are needed a new file is
created inside of the `auth` package which will define the default location,
the structure of the configuration, and a function to parse the configuration
file (usually named Read<platform>Config and emits a
`map[string]<platform>Config` object).

## API

Platform APIs wrap each cloud provider's golang SDK and live inside of
`platform/api/<platform>/`. There is no direct requirement for what
functionality is present in the API.

## Cluster & Machine

Clusters must implement the `Cluster` [interface](https://github.com/coreos/mantle/tree/master/platform/platform.go#L75-L97).
Machines must implement the `Machine` [interface](https://github.com/coreos/mantle/tree/master/platform/platform.go#L40-L73).

## Adding a new platform to the kola runner

To add a new platform to the `kola` runner the following things must be added:
 1. A platform specific struct inside of `cmd/kola/kola.go` which contains the
 fields that should be logged to give more information about a test run.
 Generally this will contain things like `version` or `ami` and `region`. [This](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L138-L142)
 is an example of the struct and [this](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L179-L183) shows the data
 being added to the output (which can be found in
 `_kola_temp/<platform>-latest/properties.json`).
 2. The platform specific options inside of `cmd/kola/options.go`
 ([for example DigitalOcean](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L179-L183)). The flags will
 generally contain an override for the configuration file location, region,
 type/size, and the image.
 3. The platform needs to be added to the `kolaPlatforms` list inside of
 `cmd/kola/options.go` [here](https://github.com/coreos/mantle/tree/master/cmd/kola/options.go#L32)
 4. The platform options & new cluster inside of `kola/harness.go`. The platform
 options variables are defined [here](https://github.com/coreos/mantle/tree/master/kola/harness.go#L54-L60) and the
 `NewCluster` method is defined [here](https://github.com/coreos/mantle/tree/master/kola/harness.go#L143-L161).

## Other things to consider adding

It is generally preferred that new platforms add garbage collection methods via
the `ore <platform> gc` command.

For platforms which support adding custom images `ore` commands to upload &
create images from the image file.

# Existing Platforms

## Aliyun

 - The Aliyun platform wraps both [alibaba-cloud-sdk-go](https://github.com/aliyun/alibaba-cloud-sdk-go) as well as [aliyun-oss-go-sdk](https://github.com/aliyun/aliyun-oss-go-sdk).

## AWS

 - The AWS platform wraps [aws-sdk-go](https://github.com/aws/aws-sdk-go).
 - By default SSH keys will be passed via both the AWS metadata AND the userdata.
 - UserData is passed to the instances via the AWS metadata service.
 - Instances are tagged with `Name:<generated name>` and `CreatedBy:mantle`. The `CreatedBy` tag is used by `GC` when searching for instances to terminate.
 - Serial Console data on AWS is only saved by the cloud during boot sequences (initial boot and all subsequent reboot / shutdowns). This means that sometimes the serial console will not be complete as only the [most recent 64KB is stored](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-console.html).
 - If a security group matching the name in `aws-sg` (default: `kola`) is not found then one will be created, along with a VPC, internet gateway, route table, and subnets.
 - Both AMI's as well as the channel names are accepted via the `aws-ami` parameter, if a channel is given it will be resolved via the release bucket's `coreos_production_ami_all.json` file.

## Azure

 - The Azure platform wraps [azure-sdk-for-go](https://github.com/Azure/azure-sdk-for-go).
 - By default SSH keys will be passed via both the Azure metadata AND the userdata.
 - UserData is passed to the instances via the Azure metadata service.
 - For each cluster to be created a Resource Group is created containing the necessary networking resources and a storage account (which will be used to store boot diagnostics for any machines in the cluster).
 - There are two types of images in Azure (published images and custom images). For using published images the channel can be passed via the `azure-sku` parameter and the version can be passed via the `azure-version` parameter. To specify a custom image you can pass the `azure-disk-uri` parameter.
 - `kola` works entirely on ARM based authentication, `ore` has methods for both ASM or ARM credentials.
 - `GC` in Azure searches for Resource Groups with a prefix of `kola-cluster` in the name.

## Azure Stack

Like Azure, but not.

## DigitalOcean

 - The DO platform wraps [godo](https://github.com/digitalocean/godo).
 - By default SSH keys will be passed via both the DO metadata AND the userdata.
 - UserData is passed to the instances via the DO metadata service.
 - DigitalOcean has no method for uploading custom images, as a result the `ore do create-image` command does a [~~terrifying~~ special workaround](https://github.com/coreos/mantle/blob/master/cmd/ore/do/create-image.go#L117-L173) which specifies custom userdata that does the following (after which the machine is snapshotted):
   - configure networking in the initramfs
   - Download a custom image
   - replaces `/root/initramfs/shutdown` with a script to:
      - unmount the root filesystem
      - dd the custom image over the disk
      - reboot
 - Instances are given the tag `mantle` which is used by `GC`.
 - The `do-image` parameter accepts snapshot ID's as well as the channel names, if a channel is given the relevant slug `coreos-<channel>` will be passed to DO.

## ESX

 - The ESX platform wraps [govmomi](https://github.com/vmware/govmomi).
 - ESX is actually a bit of a misnomer, it actually requires that the target is a vSphere instance (as it uses features not present in the free ESXi hypervisor).
 - SSH keys will be passed via userdata.
 - Userdata is passed to the instances via the `ovfenv`. When a machine is created the API updates the `config.vAppConfig.property.guestinfo.coreos.config.data` field to be a base64 encoded userdata config.
 - The general workflow for running tests on ESX is to run the `ore esx create-base` to upload an OVF image and then the resulting image name is specified to `kola` via the `esx-base-vm` parameter. This is done to only require kola to perform a clone operation rather than a full re-upload of the image.

## GCE

 - The GCE/gcloud platform wraps [this SDK](google.golang.org/api).
 - By default SSH keys will be passed via both the GCE metadata AND the userdata.
 - UserData is passed to the instances via the GCE metadata service.
 - Instances are tagged with `created-by:mantle` which is used when filtering instances for `GC`.

## OpenStack

 - The OpenStack platform wraps [gophercloud](https://github.com/gophercloud/gophercloud).
 - By default SSH keys will be passed via both the OpenStack metadata AND the userdata.
 - UserData is passed to the instances via the OpenStack metadata service.
 - Instances are tagged with `CreatedBy: mantle` which is used when filtering instances for `GC`.

## Packet

 - The Packet platform wraps [packngo](https://github.com/packethost/packngo).
 - SSH keys will be passed via userdata.
 - Custom images do not use the Packet Custom Images API but rather the machine creation actually writes a custom iPXE script (which is uploaded to Google Storage) that sets `coreos.config.url` on the kernel command-line to point at a userdata file (which is also uploaded to Google Storage). This userdata file contains multiple systemd units & file definitions -- the actual metadata is written to `/userdata`. The systemd units will run `coreos-install` to install the custom image on the machine (and pass the config file).
 - Packet provides a URL for accessing the serial console, an SSH client is created to this endpoint and the stdout is fed to the `Console` object.
 - Devices are tagged with `mantle` which is used by `GC`.

## IBMCloud

- The IBMCloud platform wraps [bluemix-go](https://github.com/IBM-Cloud/bluemix-go) and [ibm-cos-sdk-go](https://github.com/IBM/ibm-cos-sdk-go)
- The IBMCloud image is a qemu variant qcow image sized at 100GB (https://cloud.ibm.com/docs/vpc?topic=vpc-create-linux-custom-image#boot-disk-100GB)