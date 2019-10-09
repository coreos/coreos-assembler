#!/usr/bin/env bash
set -euo pipefail
# Helper library for using libguestfs on CoreOS-style images.
# A major assumption here is that the disk image uses OSTree
# and also has `boot` and `root` filesystem labels.

# We don't want to use libvirt for this, it inhibits debugging
export LIBGUESTFS_BACKEND=direct
# http://libguestfs.org/guestfish.1.html#using-remote-control-robustly-from-shell-scripts
GUESTFISH_PID=
coreos_gf_launch() {
    if [ -n "$GUESTFISH_PID" ]; then
        return
    fi
    eval "$(echo "nokey" \
             | env LIBGUESTFS_MEMSIZE=2048M \
                   LIBGUESTFS_DEBUG=1 \
                   guestfish --listen --keys-from-stdin -a "$@")"
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
# Export `stateroot` and `deploydir` variables.
coreos_gf_run_mount() {
    coreos_gf_run "$@"
    # Detect the RHCOS LUKS case; first check if there's
    # no visible "root" labeled filesystem
    local part4name
    part4name=$(coreos_gf part-get-name /dev/sda 4)
    if [ "${part4name}" = "luks_root" ]; then
        coreos_gf luks-open /dev/sda4 crypt_root
    fi
    root=$(coreos_gf findfs-label root)
    coreos_gf mount "${root}" /
    local boot
    boot=$(coreos_gf findfs-label boot)
    coreos_gf mount "${boot}" /boot
    # As far as I can tell libguestfs doesn't have a "find partition by GPT type" API,
    # let's hack this and assume it's the first if present.
    EFI_SYSTEM_PARTITION_GUID="C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
    if [ "$(coreos_gf part-get-gpt-type /dev/sda 1)" = "${EFI_SYSTEM_PARTITION_GUID}" ]; then
        coreos_gf mount /dev/sda1 /boot/efi
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
