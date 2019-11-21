#!/usr/bin/env bash
set -euo pipefail
# Helper library for using libguestfs on CoreOS-style images.
# A major assumption here is that the disk image uses OSTree
# and also has `boot` and `root` filesystem labels.

EFI_SYSTEM_PARTITION_GUID="C12A7328-F81F-11D2-BA4B-00A0C93EC93B"

# We don't want to use libvirt for this, it inhibits debugging
export LIBGUESTFS_BACKEND=direct
if [ -z "${LIBGUESTFS_MEMSIZE:-}" ]; then
    # Unfortunate hack: we need a much larger RAM size in order to use dm-crypt
    # for RHCOS.  See https://gitlab.com/cryptsetup/cryptsetup/issues/372
    # Theoretically we could run with a small size, detect, then re-launch
    # only if we detect this case, but eh.
    export LIBGUESTFS_MEMSIZE=2048
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
    # As far as I can tell libguestfs doesn't have a "find partition by GPT type" API,
    # let's hack this and assume it's the first if present.
    if [ "$(coreos_gf part-get-gpt-type /dev/sda 1)" = "${EFI_SYSTEM_PARTITION_GUID}" ]; then
        coreos_gf ${mntarg} /dev/sda1 /boot/efi
    fi

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
