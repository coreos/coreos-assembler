#!/bin/sh
# This script is run in supermin to create a Fedora CoreOS style
# disk image, very much in the spirit of the original
# Container Linux (orig CoreOS) disk layout, although adapted
# for OSTree, and using XFS for /, among other things.
# Some more background in https://github.com/coreos/fedora-coreos-tracker/issues/18
# The layout is intentionally not very configurable at this time,
# although see also https://github.com/coreos/coreos-assembler/pull/298
# For people building "derived"/custom FCOS-like systems, feel free to file
# an issue and we can discuss configuration needs.
set -euo pipefail

if [ "$#" -ne 6 ]; then
	echo 'create_disk <device> <ostree-repo> <ostree-ref> <grub-script> <os-name> <space separated kargs>'
	exit 1
fi

export PATH=$PATH:/sbin:/usr/sbin

arch="$(uname -m)"

disk="$1" && shift
ostree="$1" && shift
ref="$1" && shift
grub_script="$1" && shift
os_name="$1" && shift
extrakargs="$1" && shift

set -x

# partition and create fs
sgdisk -Z $disk \
	-n 1:0:+384M -c 1:boot \
	-n 2:0:+127M -c 2:EFI-SYSTEM -t 2:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
	-n 3:0:+1M   -c 3:BIOS-BOOT  -t 3:21686148-6449-6E6F-744E-656564454649 \
	-n 4:0:0     -c 4:root       -t 4:4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709
sgdisk -p "$disk"

udevadm trigger
udevadm settle

mkfs.ext4 "${disk}1" -L boot
mkfs.fat "${disk}2" -n EFI-SYSTEM
# partition 3 has no FS, its for bios grub
mkfs.xfs "${disk}4"  -L root -m reflink=1

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
ostree admin deploy "$ref" --sysroot rootfs --os "$os_name" $kargsargs

if [ "$arch" == "x86_64" ]; then
	# install bios grub
	grub2-install \
		--target i386-pc \
		--boot-directory rootfs/boot \
		$disk
	ext="X64"
else
	mkdir -p rootfs/boot/grub2
	ext="AA64"
fi

# install uefi grub
mkdir -p rootfs/boot/efi/EFI/{BOOT,fedora}
cp "/boot/efi/EFI/BOOT/BOOT${ext}.EFI" "rootfs/boot/efi/EFI/BOOT/BOOT${ext}.EFI"
cp "/boot/efi/EFI/fedora/grub${ext,,}.efi" "rootfs/boot/efi/EFI/BOOT/grub${ext,,}.efi"
cat > rootfs/boot/efi/EFI/fedora/grub.cfg << 'EOF'
search --label boot --set prefix
set prefix=($prefix)/grub2
normal
EOF

# copy the grub config and any other files we might need
cp $grub_script rootfs/boot/grub2/grub.cfg
touch rootfs/boot/ignition.firstboot

umount -R rootfs
