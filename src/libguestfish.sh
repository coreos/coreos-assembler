#!/usr/bin/env bash
# Helper library for using libguestfs on CoreOS-style images.
# A major assumption here is that the disk image uses OSTree
# and also has `boot` and `root` filesystem labels.

# We don't want to use libvirt for this, it inhibits debugging
export LIBGUESTFS_BACKEND=direct
# http://libguestfs.org/guestfish.1.html#using-remote-control-robustly-from-shell-scripts
GUESTFISH_PID=
coreos_gf_launch() {
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

# Run libguestfs and mount the root and boot partitions.
# Export `stateroot` and `deploydir` variables.
coreos_gf_run_mount() {
    coreos_gf_launch "$@"
    coreos_gf run
    local root
    root=$(coreos_gf findfs-label root)
    coreos_gf mount "${root}" /
    local boot
    boot=$(coreos_gf findfs-label boot)
    coreos_gf mount "${boot}" /boot
    var=$(coreos_gf -findfs-label var 2>/dev/null || true)

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
