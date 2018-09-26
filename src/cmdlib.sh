# Shared shell script library

# Global variables
export workdir=$(pwd)
export configdir=${workdir}/src/config
export manifest=${configdir}/manifest.yaml
export superminpreparedir="${workdir}/tmp/supermin-prepare.d"
export superminbuilddir="${workdir}/tmp/supermin-build.d"
export cachesimg="${workdir}/caches.qcow2"

fatal() {
    echo "error: $@" 1>&2; exit 1
}

preflight() {
    # Verify we have all dependencies
    local deps=$(grep -v '^#' /usr/lib/coreos-assembler/deps.txt)
    # Explicitly check the packages in one rpm -q to avoid
    # overhead, only drop down to individual calls if that fails.
    # We use --whatprovides so we handle file paths too.
    if ! rpm -q --whatprovides ${deps} &>/dev/null; then
        local missing=""
        for dep in ${deps}; do
            if ! rpm -q --whatprovides "${dep}" &>/dev/null; then
                missing="$missing $dep"
            fi
        done
        fatal "Failed to find expected dependencies:$missing"
    fi

    if [ $(stat -f --printf="%T" .) = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to find /dev/kvm"
    fi

#   if ! capsh --print | grep -q 'Current.*cap_sys_admin'; then
#       fatal "This container must currently be run with --privileged"
#   fi

#   if ! sudo true; then
#       fatal "The user must currently have sudo privileges"
#   fi

    # permissions on /dev/kvm vary by (host) distro.  If it's
    # not writable, recreate it.
    if ! [ -w /dev/kvm ]; then
        sudo rm -f /dev/kvm
        sudo mknod /dev/kvm c 10 232
        sudo setfacl -m u:$USER:rw /dev/kvm
    fi
}

prepare_build() {
    preflight
    if ! [ -d repo ]; then
        fatal "No $(pwd)/repo found; did you run coreos-assembler init?"
    fi

#   export workdir=$(pwd)
#   export configdir=${workdir}/src/config
#   export manifest=${configdir}/manifest.yaml
#   export superminpreparedir="${workdir}/tmp/supermin-prepare.d"
#   export superminbuilddir="${workdir}/tmp/supermin-build.d"
#   export cachesimg="${workdir}/caches.qcow2"

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    echo "Using manifest: ${manifest}"

    # Abuse the rojig/name as the name of the VM images
    export name=$(manifest_get '["rojig"]["name"]')
    # TODO - allow this to be unset
    export ref=$(manifest_get '["ref"]')

    # This dir is no longer used
    rm builds/work -rf

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p ${workdir}/tmp

    # Allocate temporary space for this build
    tmp_builddir=${workdir}/tmp/build
    rm ${tmp_builddir} -rf
    mkdir ${tmp_builddir}
    # And everything after this assumes it's in the temp builddir
    cd ${tmp_builddir}
    # *This* tmp directory is truly temporary to this build, and
    # contains artifacts we definitely don't want to outlive it, unlike
    # other things in ${workdir}/tmp.  We also export it as an environment
    # variable for child processes like gf-oemid.
    mkdir tmp
    export TMPDIR=$(pwd)/tmp
}

# We'll rewrite this in a real language I promise
manifest_get() {
    python3 -c 'import sys,yaml; print(yaml.safe_load(open(sys.argv[1]))'"$1"')' "${manifest}"
}

runcompose() {
    local treecompose_args=""
    if ! grep -q '^# disable-unified-core' "${manifest}"; then
        treecompose_args="${treecompose_args} --unified-core"
    fi
    set -x
    sudo rpm-ostree compose tree --repo=${workdir}/repo-build --cachedir=${workdir}/cache ${treecompose_args} \
         ${TREECOMPOSE_FLAGS:-} ${manifest} "$@"
    set +x
}

prepare_vm() {
    # rpms to create appliance VM out of
    rpms=' bash vim-minimal coreutils util-linux procps-ng kmod kernel-modules'
    rpms+=' cifs-utils' # for samba
    rpms+=' systemd' # for clean reboot
    rpms+=' dhcp-client bind-export-libs iproute' # networking
    rpms+=' rpm-ostree distribution-gpg-keys' # to run the compose
    rpms+=' selinux-policy selinux-policy-targeted policycoreutils' #selinux

    # prepare appliance VM
    supermin -v --prepare --use-installed $rpms -o "${superminpreparedir}"
    supermin -v --build "${superminpreparedir}" \
             --include-packagelist --size 4G -f ext2 -o "${superminbuilddir}"

    # Create a disk image for our caches (pkgcache and bare-user build repo)
    [ ! -d "${workdir}/tmp/emptydir" ] && mkdir "${workdir}/tmp/emptydir"
    if [ ! -f "${cachesimg}" ]; then
        virt-make-fs --format=qcow2 --type=xfs \
                     --size=10G "${workdir}/tmp/emptydir" "${cachesimg}"
    fi
}

run_vm() {
    init=$1
    set -x

    # Copy in the init script with the code we want to run  
    chmod +x "${init}"
    virt-copy-in -a "${superminbuilddir}/root" "${init}" /

    # Execute unprivileged VM to run compose. Some notes:
    # - smb="${workdir}" - we'll serve our workdir over samba
    # - sda - rootfs from supermin build
    # - sdb - caches.qcow2 - we store the bare-user repo and pkgcache here
    # - discard=unmap - using virtio-scsi disks and discard=unmap so we can fstrim
    #                   and recover disk space from the VM
    qemu-kvm -nodefaults -nographic -m 2048 -no-reboot \
              -kernel "${superminbuilddir}/kernel" \
              -initrd "${superminbuilddir}/initrd" \
              -netdev user,id=eth0,hostname=supermin,smb="${workdir}",hostfwd=tcp:127.0.0.1:8000-:8000 \
              -device virtio-net-pci,netdev=eth0 \
              -device virtio-scsi-pci,id=scsi0,bus=pci.0,addr=0x3 \
              -drive if=none,id=drive-scsi0-0-0-0,snapshot=on,file="${superminbuilddir}/root" \
              -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=0,drive=drive-scsi0-0-0-0,id=scsi0-0-0-0,bootindex=1 \
              -drive if=none,id=drive-scsi0-0-0-1,discard=unmap,file="${cachesimg}" \
              -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=1,drive=drive-scsi0-0-0-1,id=scsi0-0-0-1 \
              -serial stdio -append "root=/dev/sda console=ttyS0 selinux=1 enforcing=0 autorelabel=1"

    if [ -f "${workdir}/tmp/supermin-failure" ]; then
        rm -f "${workdir}/tmp/supermin-failure"
        fatal "Detected failure from compose VM run"
    fi
}

runcompose_in_vm() {
    previous_commit=$1
    ref=$2
    shift; shift;

    local treecompose_args=""
    if ! grep -q '^# disable-unified-core' "${manifest}"; then
        treecompose_args="${treecompose_args} --unified-core"
    fi

    cmd="rpm-ostree compose tree --repo=${workdir}/repo-build"
    cmd+=" --cachedir=${workdir}/cache ${treecompose_args}"
    cmd+=" ${TREECOMPOSE_FLAGS:-} ${manifest} $@"

    # populate variables in init script
    template=$(dirname $0)/supermin-init
    sed -e "s|^workdir=|workdir=\"${workdir}\"|" \
        -e "s|^previous_commit=|previous_commit=\"${previous_commit}\"|" \
        -e "s|^ref=|ref=\"${ref}\"|" \
        -e "s|^cmd=|cmd=\"${cmd}\"|" $template > "${superminbuilddir}/init"

    run_vm "${superminbuilddir}/init"
}
