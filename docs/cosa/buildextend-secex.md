---
parent: CoreOS Assembler Command Line Reference
nav_order: 1
---

# cosa buildextend-secex

This buildextend command is used to build QEMU images that are enabled for IBM Secure Execution on IBM Z.
In order to build a QEMU image protected by IBM Secure Execution, you need to provide a host key to encrypt it.

For more information on IBM Secure Execution on IBM Z, refer to the [IBM Documentation](https://www.ibm.com/docs/en/linux-on-systems?topic=ibmz-secure-execution).

The command is intended to be used in the RHCOS CI together with the universal host key, such that the image can be booted on any IBM Z machine that supports IBM Secure Execution.
This results in a few specifics to note:
- The resulting image will only be encrypted with a single host key, to enable firstboot.
- The host key will not be written to the image.
- The host key(s) need to be provided later during firstboot through Ignition.
    - The firstboot service will fail when no host key is provided, as the sdboot-image can not be recreated.
    - Write the host key(s) to: `/etc/se-hostkeys/ibm-z-hostkey-<hostkey-name>.crt`

To facilitate this, `buildextend-secex` can take 2 mutually exclusive additional arguments: `--genprotimgvm <path>` and `--hostkey <path>`.
If none is provided, `--genprotimgvm` is used with default values.

## `--genprotimgvm <path>` (default)

Default Value: `/data.secex/genprotimgvm.qcow2`

This path is the default behavior. It assumes that the host key is not directly available, but is supplied through an IBM Secure Execution protected VM only.

The QEMU image will be built normally. However, it will not run `genprotimg` or `zipl`, but instead save the required input for the command to a temporary location.
After the build, the provided VM will run.  The VM is used to isolate and protect the `genprotimg` command, so that the universal host key is not exposed.
A provided bash script is called before and after the `genprotimg` command, to fullfil the following steps:
1. Copy the required kernel, initramfs, and parmfile to the VM
2. Move the sdboot-image to the disk
3. Call `zipl`to make the image bootable.
This enables us to copy the required kernel, initramfs and parmfile to the VM and afterwards move the sdboot-image to the disk, as well as calling `zipl` to make the image bootable.

## `--hostkey <path>`

This path is intended for local development, but can be used for custom builds. The path takes a singe host key file, which is used to build the image.

Instead of running `genprotimg` and `zipl` in a separate VM, they run during the build process. Otherwise, the build is identical to the `--genprotimgvm`.
Note: It is still assumed that the host key is provided via Ignition during firstboot.
