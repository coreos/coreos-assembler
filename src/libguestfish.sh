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
    local src=$1
    shift
    local guestfish
    guestfish[0]="guestfish"
    guestfish[1]="--listen"
    guestfish[3]="-a"
    guestfish[4]="${src}"

    eval "$("${guestfish[@]}")"
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
    local root
    root=$(coreos_gf findfs-label root)
    coreos_gf mount "${root}" /
    local boot
    boot=$(coreos_gf findfs-label boot)
    coreos_gf mount "${boot}" /boot
    var=$(coreos_gf -findfs-label var 2>/dev/null || true)
    # As far as I can tell libguestfs doesn't have a "find partition by GPT type" API,
    # let's hack this and assume it's the first if present.
    EFI_SYSTEM_PARTITION_GUID="C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
    if [ "$(coreos_gf part-get-gpt-type /dev/sda 1)" = "${EFI_SYSTEM_PARTITION_GUID}" ]; then
        coreos_gf mount /dev/sda1 /boot/efi
    fi

    # Export these variables for further use
    stateroot=/ostree/deploy/$(coreos_gf ls /ostree/deploy)
    deploydir="${stateroot}"/deploy/$(coreos_gf ls "${stateroot}"/deploy | grep -v \.origin)
    if [ -n "${var}" ]; then
        # Since it doesn't look like libguestfs supports bind mounts, and other
        # code processes the stateroot directory, let's mount it there.
        coreos_gf mount "${var}" "${stateroot}"/var
    fi
    export stateroot deploydir
}

# Cleanly unmount all filesystems and terminate the helper VM.
coreos_gf_shutdown() {
    coreos_gf umount-all
    coreos_gf exit
}

# Relabel the provided file paths. You must use this when creating files that
# don't already exist, otherwise they will be `unlabeled_t`.
#
# Note that this doesn't correctly handle relabeling anything actually
# underneath the ${deploydir} because we'd need a way to strip the deploydir
# path prefix.
#
# Rather than using this function, a good trick is to `coreos_gf cp-a /path/to/existing/file /path/to/new-file`
# and then truncate/rewrite that existing file.
#
# An even better thing to do is to create the files on first boot via Ignition,
# which will handle labeling too.
#
# So basically, only create files via libguestfs that are needed to prepare
# for Ignition.
coreos_gf_relabel() {
    coreos_gf selinux-relabel ${deploydir}/etc/selinux/targeted/contexts/files/file_contexts "$@"
}
