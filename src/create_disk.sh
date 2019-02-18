#!/bin/sh

usage() {
	echo "create_disk -d disk -o ostree"
}

export PATH=$PATH:/sbin:/usr/sbin

getpart() {
	# getpart /dev/loop0 1 -> /dev/loop0p1
	# getpart /dev/sda 1 -> /dev/sda1
	last="${1: -1}"
	if [ $last -q $last 2>/dev/null ]; then
		echo "${1}p${2}"
	else
		echo "${1}${2}"
	fi
}

rc=0
TEMP=$(getopt -o "d:o:r:" --long "disk:,ostree:,ref:" -- "$@") || rc=$?
if [ "$rc" -ne 0 ]; then
	usage
	exit 1
fi

eval set -- "$TEMP"

while :
do
	case "$1" in
		"-d"|"--disk")
			shift
			disk="$1"
			shift
			;;
		"-o"|"--ostree")
			shift
			ostree="$1"
			shift
			;;
		"-r"|"--ref")
			shift
			ref="$1"
			shift
			;;
		--)
			shift
			break
			;;
		*)
			echo "Error parsing args"
			usage
			exit 1
			;;
	esac
done

[ -z "$disk" ] || [ -z "$ostree" ] && {
	usage
	exit 1
}

set -e

script_dir=$(dirname $(readlink -f "$0"))
# partition and create fs
sgdisk -Z $disk \
	-n 1:0:+128M -c 1:boot \
	-n 2:0:+128M -c 2:EFI-SYSTEM -t 1:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
	-n 3:0:+128M -c 3:BIOS-BOOT  -t 2:21686148-6449-6E6F-744E-656564454649 \
	-n 4:0:0     -c 4:root       -t 3:4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709
sgdisk -p $disk

# HACK ALERT - wait for partition rescans
sleep 2
# FIXME ostree needs symlinks
mkfs.ext2 "$(getpart ${disk} 1)" -L boot
mkfs.fat "$(getpart ${disk} 2)" -n EFI-SYSTEM
mkfs.xfs "$(getpart ${disk} 4)"  -L root

# mount the partitions
rm -rf rootfs
mkdir rootfs
mount $(getpart ${disk} 4) rootfs
mkdir rootfs/boot
mount $(getpart ${disk} 1) rootfs/boot
mkdir rootfs/boot/efi
mount $(getpart ${disk} 2) rootfs/boot/efi

# init the ostree
ostree admin init-fs rootfs
ostree pull-local "$ostree" --repo rootfs/ostree/repo
ostree admin os-init fedora-coreos --sysroot rootfs
ostree admin deploy "$ref" --sysroot rootfs --os fedora-coreos

checksum=$(cat rootfs/boot/ostree/*/{vm*,init*} | sha256sum | cut -d ' ' -f 1)
vmlinuz=$(basename  rootfs/boot/ostree/*/vm*)
initrd=$(basename rootfs/boot/ostree/*/init*)
echo "Checksum is: $checksum initrd is $initrd vmlinuz is $vmlinuz"

# install bios grub (mostly lifted from the container linux scripts)
grub2-install \
	--target i386-pc \
	--boot-directory rootfs/boot \
	$disk

#mkdir -p rootfs/boot/efi/EFI/boot
#grub2-mkimage \
#	--format x86_64-efi \
#	--output rootfs/boot/efi/EFI/grub.efi \
#	--prefix='(root)/boot/grub'
#	serial linuxefi efi_gop getenv smbios efinet verify http tftp

cat "$script_dir/grub.cfg" \
	| sed "s/HASHHASH/$checksum/" \
	| sed "s/VMLINUZ/$vmlinuz/" \
	| sed "s/INITRD/$initrd/" \
	| tee rootfs/boot/grub2/grub.cfg

touch rootfs/boot/ignition.firstboot
