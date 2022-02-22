#!/usr/bin/env bash
set -euo pipefail
# Helper library for using libguestfs on CoreOS-style images.
# A major assumption here is that the disk image uses OSTree
# and also has `boot` and `root` filesystem labels.


# We don't want to use libvirt for this, it inhibits debugging
# shellcheck disable=SC2031
export LIBGUESTFS_BACKEND=direct

arch=$(uname -m)

if [ "$arch" = "ppc64le" ] ; then
    export LIBGUESTFS_HV="/usr/lib/coreos-assembler/libguestfs-ppc64le-wrapper.sh"
fi

# http://libguestfs.org/guestfish.1.html#using-remote-control-robustly-from-shell-scripts
GUESTFISH_PID=
coreos_gf_launch() {
    if [ -n "$GUESTFISH_PID" ]; then
        return
    fi

    eval "$(guestfish --listen -a "$@")"
    if [ -z "$GUESTFISH_PID" ]; then
        fatal "guestfish didn't start up, see error messages above"
    fi
}

_coreos_gf_cleanup () {
    guestfish --remote -- exit >/dev/null 2>&1 ||:
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
