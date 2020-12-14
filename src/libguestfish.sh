#!/usr/bin/env bash
set -euo pipefail
# Helper library for using libguestfs on CoreOS-style images.
# A major assumption here is that the disk image uses OSTree
# and also has `boot` and `root` filesystem labels.


# We don't want to use libvirt for this, it inhibits debugging
export LIBGUESTFS_BACKEND=direct
if [ -z "${LIBGUESTFS_MEMSIZE:-}" ]; then
    # Unfortunate hack: we need a much larger RAM size in order to use dm-crypt
    # for RHCOS.  See https://gitlab.com/cryptsetup/cryptsetup/issues/372
    # Theoretically we could run with a small size, detect, then re-launch
    # only if we detect this case, but eh.
    export LIBGUESTFS_MEMSIZE=2048
fi

arch=$(uname -m)

if [ "$arch" = "ppc64le" ] ; then
    tmp_qemu_wrapper=$(mktemp -tdp /tmp gf-vsmt.XXXXXX)
    qemu_wrapper=${tmp_qemu_wrapper}/qemu-wrapper.sh
	cat <<-'EOF' > "${qemu_wrapper}"
	#!/bin/bash -
	exec qemu-system-ppc64 "$@" -machine pseries,accel=kvm:tcg,vsmt=8,cap-fwnmi=off
	EOF
    chmod +x "${qemu_wrapper}"
    export LIBGUESTFS_HV="${qemu_wrapper}"
fi

# http://libguestfs.org/guestfish.1.html#using-remote-control-robustly-from-shell-scripts
GUESTFISH_PID=
coreos_gf_launch() {
    if [ -n "$GUESTFISH_PID" ]; then
        return
    fi

    eval "$(echo nokey | guestfish --listen --keys-from-stdin --key /dev/sda4:file:/dev/null -a "$@")"
    if [ -z "$GUESTFISH_PID" ]; then
        fatal "guestfish didn't start up, see error messages above"
    fi
}

_coreos_gf_cleanup () {
    guestfish --remote -- exit >/dev/null 2>&1 ||:
    if [ -n "${tmp_qemu_wrapper:-}" ] ; then
        rm -rf "${tmp_qemu_wrapper}";
    fi
}
trap _coreos_gf_cleanup EXIT

coreos_gf() {
    guestfish --remote -- "$@"
}

GUESTFISH_RUNNING=
coreos_gf_run() {
    if [ -n "$GUESTFISH_RUNNING" ]; then
        return
    fi
    coreos_gf_launch "$@"
    coreos_gf run
    GUESTFISH_RUNNING=1
}

# Run libguestfs and mount the root and boot partitions.
# Export `stateroot` and `deploydir` variables.  The
# special option `ro` (if provided first) mounts read-only
# by default.
coreos_gf_run_mount() {
    local mntarg=mount
    if [ "$1" = ro ]; then
        mntarg=mount-ro
        shift
    fi
    coreos_gf_run "$@"
    # Detect the RHCOS LUKS case; first check if there's
    # no visible "root" labeled filesystem
    local fstype
    fstype=$(coreos_gf vfs-type /dev/sda4)
    if [ "${fstype}" = "crypto_LUKS" ]; then
        local luksopenarg=luks-open
        if [ "${mntarg}" = "mount-ro" ]; then
            luksopenarg=luks-open-ro
        fi
        coreos_gf "${luksopenarg}" /dev/sda4 luks-00000000-0000-4000-a000-000000000002
    fi
    root=$(coreos_gf findfs-label root)
    coreos_gf ${mntarg} "${root}" /
    local boot
    boot=$(coreos_gf findfs-label boot)
    coreos_gf ${mntarg} "${boot}" /boot
    # ESP, if it exists
    local partitions
    local label
    partitions="$(coreos_gf list-partitions)"
    for pt in $partitions; do
        label="$(coreos_gf vfs-label "${pt}")"
        if [ "$label" == "EFI-SYSTEM" ]; then
            coreos_gf ${mntarg} "${pt}" /boot/efi
        fi
    done

    # Export these variables for further use
    stateroot=/ostree/deploy/$(coreos_gf ls /ostree/deploy)
    deploydir="${stateroot}"/deploy/$(coreos_gf ls "${stateroot}"/deploy | grep -v \.origin)
    export stateroot deploydir
}

# Cleanly unmount all filesystems and terminate the helper VM.
coreos_gf_shutdown() {
    coreos_gf umount-all
    coreos_gf exit
    GUESTFISH_RUNNING=
    GUESTFISH_PID=
}
