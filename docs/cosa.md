---
layout: default
has_children: true
nav_order: 5
---

# CoreOS Assembler Command Line Reference

This is a short reference of `cosa` sub-commands available in a CoreOS
Assembler container. See each commands `--help` output for more details about
supported arguments.

## Main commands

| Name | Description |
| [build](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-build) | Build OSTree and image base artifacts from previously fetched packages
| [clean](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-clean) | Delete all build artifacts
| [fetch](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-fetch) | Fetch and import the latest packages
| [init](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-init) | Setup the current working directory for CoreOS Assembler and clone the given project URL as Git config
| [kola](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-kola) | Run tests with [kola](kola.md)
| [list](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-list) | List builds available locally
| [run](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-run) | Run a CoreOS instance in QEMU with access to a root shell
| [shell](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-shell) | Get an interactive shell or run a command in a CoreOS Assembler container
| [virt-install](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-virt-install) | "Install" a CoreOS system with libvirt

The following commands only do a partial rebuild to make it faster to develop
on a specific part of the OS. Make sure to use the one corresponding to the
part that you are working on or you will not benefit from them (i.e. they will
not produce an image with your changes):

| Name | Description |
| [build-fast](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-build-fast) | Creates a new QCOW2 image from an existing build and updates the ostree commit with local overrides. This will only change files located in the final root (i.e. part of an ostree commit).
| [buildinitramfs-fast](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildinitramfs-fast) | Create a new QCOW2 image from an existing build and updates the initramfs with local overrides. This will not re-run dracut to rebuild the initramfs.

## buildextend commands

By default, the `build` command will build an OSTree and a QEMU image as base
artifcats. Those commands extend those artifacts to make them functionnal on
other platforms or cloud providers:

| Name | Description |
| buildextend-{aliyun,aws,azure,digitalocean,exoscale,gcp,vultr} | [ore-wrapper](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-ore-wrapper)
| aliyun-replicate, aws-replicate | [ore-wrapper](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-ore-wrapper)
| buildextend-{azurestack,ibmcloud,openstack,vmware} | [artifact-disk](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-artifact-disk)
| buildextend-{dasd,metal,metal4k,qemu} | [buildextend-metal](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildextend-metal)
| buildextend-live | [buildextend-live](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildextend-live)
| buildextend-rojig | [buildextend-rojig](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildextend-rojig)

## Misc commands

Those less commonly used commands are listed here:

| Name | Description |
| [basearch](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-basearch) | Convenient wrapper for getting the base architecture
| [build-validate](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-build-validate) | Validate the checksum of a given build
| [buildprep](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildprep) | Fetches the bare minimum from external servers to create the next build
| [buildupload](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-buildupload) | Upload a build which later can be partially re-downloaded with cmd-buildprep
| [compress](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-compress) | Compresses all images in a build
| [dev-overlay](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-dev-overlay) | Add content on top of a commit, handling SELinux labeling etc.
| [dev-synthesize-osupdate](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-dev-synthesize-osupdate) | Synthesize an OS update by modifying ELF files in a "benign" way (adding an ELF note)
| [dev-synthesize-osupdatecontainer](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-dev-synthesize-osupdatecontainer) | Wrapper for dev-synthesize-osupdate that operates on an oscontainer for OpenShift
| [koji-upload](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-koji-upload) | Performs the required steps to make COSA a Koji Content Generator
| [meta](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-meta) | Helper for interacting with a builds meta.json
| [oc-adm-release](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-oc-adm-release) | Publish an oscontainer as the machine-os-content in an OpenShift release series
| [offline-update](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-offline-update) | Given a disk image and a coreos-assembler build, use supermin to update the disk image to the target OSTree commit "offline"
| [prune](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-prune) | This script removes previous builds. DO NOT USE on production pipelines
| [remote-prune](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-remote-prune) | Removes unreferenced builds from s3 bucket
| [runc](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-runc) | Spawn the current build as a container
| [sign](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-sign) | Implements signing with RoboSignatory via fedora-messaging
| [supermin-shell](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-supermin-shell) | Get a supermin shell
| [tag](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-tag) | Operate on the tags in `builds.json`
| [test-coreos-installer](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-test-coreos-installer) | Automate an end-to-end run of coreos-installer with the metal image
| [upload-oscontainer](https://github.com/coreos/coreos-assembler/blob/master/src/cmd-upload-oscontainer) | Upload an oscontainer (historical wrapper for `cosa oscontainer`)
