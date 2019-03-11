#!/usr/bin/env bash
mount -t proc /proc /proc
mount -t sysfs /sys /sys
mount -t devtmpfs devtmpfs /dev

# need /dev/shm for podman
mkdir -p /dev/shm
mount -t tmpfs tmpfs /dev/shm

# load selinux policy
LANG=C /sbin/load_policy  -i

# load kernel module for 9pnet_virtio for 9pfs mount
/sbin/modprobe 9pnet_virtio

# need fuse module for rofiles-fuse/bwrap during post scripts run
/sbin/modprobe fuse

# set up networking
/usr/sbin/dhclient eth0

# set the umask so that anyone in the group can rwx
umask 002

# set up workdir
mkdir -p "${workdir:?}"
mount -t 9p -o rw,trans=virtio,version=9p2000.L workdir "${workdir}"
if [ -L "${workdir}"/src/config ]; then
    mkdir -p "$(readlink "${workdir}"/src/config)"
    mount -t 9p -o rw,trans=virtio,version=9p2000.L source "${workdir}"/src/config
fi
mkdir -p "${workdir}"/cache /host/container-work
mount /dev/sdb1 "${workdir}"/cache

if [ -f "${workdir}/tmp/supermin/supermin.env" ]; then
    source "${workdir}/tmp/supermin/supermin.env";
fi

# /usr/sbin/ip{,6}tables is installed as a symlink to /etc/alternatives/ip{,6}tables but
# the /etc/alternatives symlink to /usr/sbin/ip{,6}tables-legacy is missing.  This recreates
# the missing link.  Hehe.
update-alternatives --install /etc/alternatives/iptables iptables /usr/sbin/iptables-legacy 1
update-alternatives --install /etc/alternatives/ip6tables ip6tables /usr/sbin/ip6tables-legacy 1

# https://github.com/koalaman/shellcheck/wiki/SC2164
cd "${workdir}" || exit
