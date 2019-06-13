#!/bin/sh
set -euo pipefail

if [ "$#" -ne 6 ]; then
	echo 'create_disk <device> <ostree-repo> <ostree-ref> <grub-script> <os-name> <space separated kargs>'
	exit 1
fi

export PATH=$PATH:/sbin:/usr/sbin

disk="$1" && shift
ostree="$1" && shift
ref="$1" && shift
grub_script="$1" && shift
os_name="$1" && shift
extrakargs="$1" && shift

# partition and create fs
sgdisk -Z $disk \
	-n 1:0:+128M -c 1:boot \
	-n 2:0:+128M -c 2:EFI-SYSTEM -t 2:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
	-n 3:0:+128M -c 3:BIOS-BOOT  -t 3:21686148-6449-6E6F-744E-656564454649 \
	-n 4:0:0     -c 4:root       -t 4:4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709
sgdisk -p "$disk"

# HACK ALERT - wait for partition rescans
sleep 2

mkfs.ext4 "${disk}1" -L boot
mkfs.fat "${disk}2" -n EFI-SYSTEM
# partition 3 has no FS, its for bios grub
mkfs.xfs "${disk}4"  -L root

# mount the partitions
rm -rf rootfs
mkdir rootfs
mount "${disk}4" rootfs
mkdir rootfs/boot
mount "${disk}1" rootfs/boot
mkdir rootfs/boot/efi
mount "${disk}2" rootfs/boot/efi

# init the ostree
ostree admin init-fs rootfs
ostree pull-local "$ostree" "$ref" --repo rootfs/ostree/repo
ostree admin os-init "$os_name" --sysroot rootfs
allkargs='root=/dev/disk/by-label/root rootflags=defaults,prjquota rw $ignition_firstboot'
allkargs="$allkargs $extrakargs"
kargsargs=""
for karg in $allkargs
do
	kargsargs+="--karg-append=$karg "
done
ostree admin deploy "$ref" --sysroot rootfs --os fedora-coreos $kargsargs

# install bios grub
grub2-install \
	--target i386-pc \
	--boot-directory rootfs/boot \
	$disk

# copy the grub config and any other files we might need
cp $grub_script rootfs/boot/grub2/grub.cfg
touch rootfs/boot/ignition.firstboot

umount -R rootfs
