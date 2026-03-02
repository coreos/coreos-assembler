#!/usr/bin/env bash
# This script is our half-baked ad-hoc reimplementation of a minimal
# Linux boot environment that we generate via `supermin`.  At some
# point we will likely switch to using systemd.

mount -t proc /proc /proc
mount -t sysfs /sys /sys
mount -t cgroup2 cgroup2 -o rw,nosuid,nodev,noexec,relatime,seclabel,nsdelegate,memory_recursiveprot /sys/fs/cgroup
mount -t devtmpfs devtmpfs /dev

# this is also normally set up by systemd in early boot
ln -s /proc/self/fd/0 /dev/stdin
ln -s /proc/self/fd/1 /dev/stdout
ln -s /proc/self/fd/2 /dev/stderr

# need /dev/shm for podman
mkdir -p /dev/shm
mount -t tmpfs tmpfs /dev/shm

# load selinux policy
LANG=C /sbin/load_policy  -i


# need fuse module for rofiles-fuse/bwrap during post scripts run
/sbin/modprobe fuse

# we want /dev/disk symlinks for coreos-installer
/usr/lib/systemd/systemd-udevd --daemon
# We've seen this hang before, so add a timeout. This is best-effort anyway, so
# let's not fail on it.
timeout 30s /usr/sbin/udevadm trigger --settle || :

# set up networking
if [ -z "${RUNVM_NONET:-}" ]; then
    /usr/sbin/dhclient eth0
fi

# set the umask so that anyone in the group can rwx
umask 002

# set up workdir
# https://github.com/coreos/coreos-assembler/issues/2171
mkdir -p "${workdir:?}"
mount -t virtiofs -o rw workdir "${workdir}"

# This loop pairs with virtfs setups for qemu in cmdlib.sh.  Keep them in sync.
for maybe_symlink in "${workdir}"/{src/config,src/yumrepos}; do
    if [ -L "${maybe_symlink}" ]; then
        bn=$(basename "${maybe_symlink}")
        mkdir -p "$(readlink "${maybe_symlink}")"
        mount -t virtiofs -o ro "/cosa/src/${bn}" "${maybe_symlink}"
    fi
done

cachedev=$(blkid -lt LABEL=cosa-cache -o device || true)
if [ -n "${cachedev}" ]; then
    mount "${cachedev}" "${workdir}"/cache
    # Also set up container storage on the cache. We use a symlink
    # rather than configuring graphroot in containers/storage.conf
    # because when osbuild runs it will use the /etc/containers/storage.conf
    # from the host (if using host as buildroot) and then will run out
    # of space in "${workdir}"/cache/cache-containers-storage inside
    # the bwrap environment. Doing it with a symlink means we can
    # still use the cache from the host, but inside osbuild it will
    # just get a blank /var/lib/containers to operate on.
    mkdir -p "${workdir}"/cache/cache-containers-storage
    rm -rf /var/lib/containers
    ln -s "${workdir}"/cache/cache-containers-storage /var/lib/containers
    # Prune all containers and images more than a few days old. Our
    # inputs here change daily so this should be reasonable.
    podman system prune --all --force --filter until=72h
else
    echo "No cosa-cache filesystem found!"
fi

if [ -f "${workdir}/tmp/supermin/supermin.env" ]; then
    source "${workdir}/tmp/supermin/supermin.env";
fi

# Code can check this to see whether it's being run via supermin; in
# some cases it may be convenient to have files wrap themselves, and
# this disambiguates between container and supermin.
touch /etc/cosa-supermin

# /usr/sbin/ip{,6}tables is installed as a symlink to /etc/alternatives/ip{,6}tables but
# the /etc/alternatives symlink to /usr/sbin/ip{,6}tables-nft is missing. This recreates
# the missing link.
update-alternatives --install /etc/alternatives/iptables iptables /usr/sbin/iptables-nft 1
update-alternatives --install /etc/alternatives/ip6tables ip6tables /usr/sbin/ip6tables-nft 1

# To build the disk image using osbuild and bootc install to-filesystem we need to
# have a prepare-root config in the build environnement for bootc to read.
# This workaround can be removed when https://github.com/bootc-dev/bootc/issues/1410
# is fixed or we have python in all streams which allows us to use the OCI image as the buildroot.
# Note that RHCOS and SCOS use the OCI as buildroot so they should not be affected by this.
cat > /usr/lib/ostree/prepare-root.conf <<EOF
[composefs]
enabled = true
EOF

# Tell bootc to enforce that `/etc/containers/policy.json` include a default
# policy that verify our images signature.
# When moving to image-builder, this config can be moved into the container itself
# but as long as we are using osbuild manually we have to carry this in the buildroot.
# TODO: uncomment this when https://github.com/bootc-dev/bootc/pull/2116
# is merged and released
# cat > usr/lib/bootc/install/10-sigpolicy.toml <<EOF
# [install]
# enforce-container-sigpolicy = true
# EOF

# TODO move this to an overlay in fedora-coreos-config
# so it get baked into the container at build time. We
# want the container to be the source of truth as much as possible.
# Same as the entries above, we need to have this in cosa until
# we move to image builder
cat <<EOF > /usr/lib/bootc/install/10-grub-users.toml
[install.ostree]
bls-append-except-default = 'grub_users=""'
EOF
