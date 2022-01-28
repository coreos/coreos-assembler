---
parent: CoreOS Assembler Command Line Reference
nav_order: 2
---

# cosa run

You can use `cosa run` (actually a thin wrapper around `kola qemuexec`) to
quickly bring up VMs locally using QEMU. In its simplest invocation, `cosa run`
by itself will start a VM using the latest QEMU image in the cosa workdir and
auto-login as `core`. You can point at a different image using `--qemu-image`:

```
$ cosa run --qemu-image rhcos-qemu.qcow2
```

Many additional options are supported. Use `--help` to see them. The following
sections discuss a few in more details.

## Testing Butane or Ignition configs

Using `cosa run` is a very effective way to iterate on your Butane or Ignition
config. Use the `--butane/-B` or `--ignition/-i` switches respectively to pass
the config path.

## Using the serial console

By default, an SSH connection is established. It's sometimes useful to see the
full Ignition run or interrupt the GRUB menu for testing. Use `-c` to use the
serial console instead:

```
$ cosa run -c
...
SeaBIOS (version 1.15.0-1.fc35)

iPXE (http://ipxe.org) 00:04.0 C000 PCI2.10 PnP PMM+3FF8C100+3FECC100 C000

Booting from Hard Disk...
..

<GRUB MENU>
...
```

To exit from the VM, use `Ctrl-A X`.

## Running the ISO

You can run the ISO using:

```
$ cosa run --qemu-iso rhcos.iso
```

## Additional disks

You can attach additional disks to the VM. This is useful for example to test an
Ignition config which partitions things there. The option can be repeated
multiple times.

The additional disks will show up as `/dev/disk/by-id/virtio-disk[N]` with N
being a 1-based index corresponding to the order in which `--add-disk` was
provided.

```
$ cosa run --add-disk 1G --add-disk 2G -B config-partition.bu
...
[core@cosa-devsh ~]$ lsblk -d
NAME MAJ:MIN RM SIZE RO TYPE MOUNTPOINT
vda  252:0    0   1G  0 disk
vdb  252:16   0   2G  0 disk
vdc  252:32   0  16G  0 disk
[core@cosa-devsh ~]$ ls -l /dev/disk/by-id
total 0
lrwxrwxrwx. 1 root root  9 Jan 28 19:23 virtio-disk1 -> ../../vda
lrwxrwxrwx. 1 root root  9 Jan 28 19:23 virtio-disk2 -> ../../vdb
lrwxrwxrwx. 1 root root  9 Jan 28 19:23 virtio-primary-disk -> ../../vdc
lrwxrwxrwx. 1 root root 10 Jan 28 19:23 virtio-primary-disk-part1 -> ../../vdc1
lrwxrwxrwx. 1 root root 10 Jan 28 19:23 virtio-primary-disk-part2 -> ../../vdc2
lrwxrwxrwx. 1 root root 10 Jan 28 19:23 virtio-primary-disk-part3 -> ../../vdc3
lrwxrwxrwx. 1 root root 10 Jan 28 19:23 virtio-primary-disk-part4 -> ../../vdc4
```

Additional disks CLI arguments support optional flags using the `--add-disk
2G:OPT1,OPT2,...` syntax. An example is `mpath`, discussed below.

## Additional kernel arguments

You can append more kernel arguments using `--kargs`:

```
$ cosa run --kargs 'foo bar'
...
[core@cosa-devsh ~]$ grep -o 'foo bar' /proc/cmdline
foo bar
```

## Simulating a CoreOS install

With `--qemu-iso` and `--add-disk`, it's possible to run through the interactive
installation flow:

```
$ cosa run -c --qemu-iso fedora-coreos-34.20211031.3.0-live.x86_64.iso --add-disk 10G
...
[core@cosa-devsh ~]$ sudo coreos-installer install /dev/vda --ignition /run/ignition.json
Installing Fedora CoreOS 34.20211031.3.0 x86_64 (512-byte sectors)
...
[core@cosa-devsh ~]$ sudo reboot # reboot into installed system
...
```

(The `--ignition /run/ignition.json` is a trick for getting auto-login on the
installed system automatically just as the live environment itself was set up.)

Of course, one can also use an Ignition config or [a customized ISO](https://coreos.github.io/coreos-installer/cmd/iso/#coreos-installer-iso-customize)
or the `coreos.inst.*` kargs using `--kargs` to also manually test automated
flows. (Many of these flows are covered by our `kola testiso` scenarios.)

## Multipath

### As primary disk

To test multipath on the primary disk in a QEMU instance, use
`--qemu-multipath`:

```
$ cosa run --qemu-multipath
...
[core@cosa-devsh ~]$ findmnt /sysroot
TARGET   SOURCE              FSTYPE OPTIONS
/sysroot /dev/mapper/mpatha4 xfs    ro,relatime,seclabel,attr2,inode64,logbufs=8,logbsize=32k,noquota
```

### As secondary disk

To test multipath on secondary disks:

```
$ cosa run --add-disk 1G:mpath
...
[core@cosa-devsh ~]$ lsblk /dev/sd*
NAME MAJ:MIN RM SIZE RO TYPE MOUNTPOINT
sda    8:0    0   1G  0 disk
sdb    8:16   0   1G  0 disk
[core@cosa-devsh ~]$ sudo mpathconf --enable
[core@cosa-devsh ~]$ sudo systemctl start multipathd
[core@cosa-devsh ~]$ lsblk /dev/sd*
NAME     MAJ:MIN RM SIZE RO TYPE  MOUNTPOINT
sda        8:0    0   1G  0 disk
`-mpatha 253:0    0   1G  0 mpath
sdb        8:16   0   1G  0 disk
`-mpatha 253:0    0   1G  0 mpath
```

This could be used for example to test a Butane config which formats and mounts
the multipathed disk at e.g. `/var/lib/containers`, or elsewhere. (This is
equivalent to the `multipath.partition` kola test.)

### As installation target

To test an installation on a multipath device:

```
$ cosa run -c --qemu-iso fedora-coreos-34.20211031.3.0-live.x86_64.iso --add-disk 10G:mpath
...
[core@cosa-devsh ~]$ sudo mpathconf --enable
[core@cosa-devsh ~]$ sudo systemctl start multipathd
[core@cosa-devsh ~]$ sudo coreos-installer install /dev/mapper/mpatha \
    --ignition /run/ignition.json \
    --append-karg rd.multipath=default \
    --append-karg root=/dev/disk/by-label/dm-mpath-root \
    --append-karg rw
Installing Fedora CoreOS 34.20211031.3.0 x86_64 (512-byte sectors)
...
[core@cosa-devsh ~]$ sudo reboot # reboot into installed system
...
[core@cosa-devsh ~]$ findmnt /sysroot
TARGET   SOURCE              FSTYPE OPTIONS
/sysroot /dev/mapper/mpatha4 xfs    ro,relatime,seclabel,attr2,inode64,logbufs=8
```

(The `--ignition /run/ignition.json` is a trick for getting auto-login on the
installed system automatically just as the live environment itself was set up.)

This is equivalent to our `kola testiso` multipath scenarios.
