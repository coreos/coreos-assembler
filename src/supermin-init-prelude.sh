#!/usr/bin/env bash
mount -t proc /proc /proc
mount -t sysfs /sys /sys
mount -t devtmpfs devtmpfs /dev

# load selinux policy
LANG=C /sbin/load_policy  -i

# load kernel module for 9pnet_virtio for 9pfs mount
/sbin/modprobe 9pnet_virtio

# need fuse module for rofiles-fuse/bwrap during post scripts run
/sbin/modprobe fuse

# set up networking
/usr/sbin/dhclient eth0

# set up workdir
mkdir -p "${workdir:?}"
mount -t 9p -o rw,trans=virtio,version=9p2000.L workdir "${workdir}"
if [ -L "${workdir}"/src/config ]; then
    mkdir -p "$(readlink "${workdir}"/src/config)"
    mount -t 9p -o rw,trans=virtio,version=9p2000.L source "${workdir}"/src/config
fi
mkdir -p "${workdir}"/cache
mount /dev/sdb1 "${workdir}"/cache

# https://github.com/koalaman/shellcheck/wiki/SC2164
cd "${workdir}" || exit
