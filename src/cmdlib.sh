#!/usr/bin/env bash
set -euo pipefail
# Shared shell script library

DIR=$(dirname "$0")

# Set PYTHONUNBUFFERED=1 so that we get unbuffered output. We should
# be able to do this on the shebang lines but env doesn't support args
# right now. In Fedora we should be able to use the `env -S` option.
export PYTHONUNBUFFERED=1

# Detect what platform we are on
if grep -q '^Fedora' /etc/redhat-release; then
    export ISFEDORA=1
    export ISEL=''
elif grep -q '^Red Hat' /etc/redhat-release; then
    export ISFEDORA=''
    export ISEL=1
else
    echo 1>&2 "should be on either RHEL or Fedora"
    exit 1
fi

info() {
    echo "info: $*" 1>&2
}

fatal() {
    echo "fatal: $*" 1>&2; exit 1
}

_privileged=
has_privileges() {
    if [ -z "${_privileged:-}" ]; then
        if [ -n "${FORCE_UNPRIVILEGED:-}" ]; then
            info "Detected FORCE_UNPRIVILEGED; using virt"
            _privileged=0
        elif ! capsh --print | grep -q 'Current.*cap_sys_admin'; then
            info "Missing CAP_SYS_ADMIN; using virt"
            _privileged=0
        elif ! sudo true; then
            info "Missing sudo privs; using virt"
            _privileged=0
        else
            _privileged=1
        fi
    fi
    [ ${_privileged} == 1 ]
}

preflight() {
    # Verify we have all dependencies
    local deps
    [ -n "${ISFEDORA}" ] && filter='^#FEDORA '
    [ -n "${ISEL}" ]     && filter='^#EL7 '
    deps=$(sed "s/${filter}//" /usr/lib/coreos-assembler/deps.txt | grep -v '^#')
    # Explicitly check the packages in one rpm -q to avoid
    # overhead, only drop down to individual calls if that fails.
    # We use --whatprovides so we handle file paths too.
    #
    # We actually want this var to be split on words
    # shellcheck disable=SC2086
    if ! rpm -q --whatprovides ${deps} &>/dev/null; then
        local missing=""
        for dep in ${deps}; do
            if ! rpm -q --whatprovides ${dep} &>/dev/null; then
                missing="$missing $dep"
            fi
        done
        fatal "Failed to find expected dependencies: $missing"
    fi

    if [ "$(stat -f --printf="%T" .)" = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to find /dev/kvm"
    fi

    # permissions on /dev/kvm vary by (host) distro.  If it's
    # not writable, recreate it.
    if ! [ -w /dev/kvm ]; then
        if ! has_privileges; then
            fatal "running unprivileged, and /dev/kvm not writable"
        else
            sudo rm -f /dev/kvm
            sudo mknod /dev/kvm c 10 232
            sudo setfacl -m u:"$USER":rw /dev/kvm
        fi
    fi

    if ! has_privileges && [ -n "${ISEL}" ]; then
        fatal "running on EL requires privileged mode"
    fi
}

prepare_build() {
    preflight
    if ! [ -d repo ]; then
        fatal "No $(pwd)/repo found; did you run coreos-assembler init?"
    elif ! has_privileges && [ ! -f cache/cache.qcow2 ]; then
        fatal "No cache.qcow2 found; did you run coreos-assembler init?"
    fi

    workdir="$(pwd)"
    configdir=${workdir}/src/config
    manifest=${configdir}/manifest.yaml
    export workdir configdir manifest

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    echo "Using manifest: ${manifest}"

    manifest_tmp_json=${workdir}/tmp/manifest.json
    rpm-ostree compose tree --repo=repo --print-only "${manifest}" > "${manifest_tmp_json}"

    # Abuse the rojig/name as the name of the VM images
    name=$(jq -r '.rojig.name' < "${manifest_tmp_json}")
    # TODO - allow this to be unset
    ref=$(jq -r '.ref' < "${manifest_tmp_json}")
    export name ref
    rm -f "${manifest_tmp_json}"

    # This dir is no longer used
    rm builds/work -rf

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p "${workdir}"/tmp

    # Needs to be absolute for rpm-ostree today
    changed_stamp=$(pwd)/tmp/treecompose.changed
    export changed_stamp

    # Allocate temporary space for this build
    tmp_builddir=${workdir}/tmp/build
    rm "${tmp_builddir}" -rf
    mkdir "${tmp_builddir}"
    # And everything after this assumes it's in the temp builddir
    # In case `cd` fails:  https://github.com/koalaman/shellcheck/wiki/SC2164
    cd "${tmp_builddir}" || exit
    # *This* tmp directory is truly temporary to this build, and
    # contains artifacts we definitely don't want to outlive it, unlike
    # other things in ${workdir}/tmp. But we don't export it since e.g. if it's
    # over an NFS mount (like a PVC in OCP), some apps might error out.
    mkdir tmp && TMPDIR=$(pwd)/tmp
}

runcompose() {
    # Implement support for automatic local overrides:
    # https://github.com/coreos/coreos-assembler/issues/118
    local overridesdir=${workdir}/overrides
    if [ -d "${overridesdir}"/rpm ]; then
        (cd "${overridesdir}"/rpm && createrepo_c .)
        echo "Using RPM overrides from: ${overridesdir}/rpm"
        local tmp_overridesdir=${TMPDIR}/override
        mkdir "${tmp_overridesdir}"
        cat > "${tmp_overridesdir}"/coreos-assembler-override-manifest.yaml <<EOF
include: ${workdir}/src/config/manifest.yaml
repos:
  - coreos-assembler-local-overrides
EOF
        # Because right now rpm-ostree doesn't look for .repo files in
        # each included dir.
        # https://github.com/projectatomic/rpm-ostree/issues/1628
        cp "${workdir}"/src/config/*.repo "${tmp_overridesdir}"/
        cat > "${tmp_overridesdir}"/coreos-assembler-local-overrides.repo <<EOF
[coreos-assembler-local-overrides]
name=coreos-assembler-local-overrides
baseurl=file://${workdir}/overrides/rpm
gpgcheck=0
EOF
        manifest=${tmp_overridesdir}/coreos-assembler-override-manifest.yaml
    fi

    rm -f "${changed_stamp}"

    set - rpm-ostree compose tree --repo="${workdir}"/repo \
            --cachedir="${workdir}"/cache --touch-if-changed "${changed_stamp}" \
            --unified-core "${manifest}" "$@"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        sudo "$@"
        sudo chown -R -h "${USER}":"${USER}" "${workdir}"/repo
    else
        runvm "$@"
    fi
}

runvm() {
    local vmpreparedir=${workdir}/tmp/supermin.prepare
    local vmbuilddir=${workdir}/tmp/supermin.build

    rm -rf "${vmpreparedir}" "${vmbuilddir}"
    mkdir -p "${vmpreparedir}" "${vmbuilddir}"

    local rpms=
    # then add all the base deps
    # for syntax see: https://github.com/koalaman/shellcheck/wiki/SC2031
    while IFS= read -r dep; do rpms+="$dep "; done < <(grep -v '^#' "${DIR}"/vmdeps.txt)
    # shellcheck disable=SC2086
    supermin --prepare --use-installed -o "${vmpreparedir}" $rpms

    # the reason we do a heredoc here is so that the var substition takes
    # place immediately instead of having to proxy them through to the VM
    cat > "${vmpreparedir}/init" <<EOF
#!/bin/bash
set -xeuo pipefail
workdir=${workdir}
$(cat "${DIR}"/supermin-init-prelude.sh)
rc=0
sh ${TMPDIR}/cmd.sh || rc=\$?
echo \$rc > ${workdir}/tmp/rc
/sbin/fstrim -v ${workdir}/cache
/sbin/reboot -f
EOF
    chmod a+x "${vmpreparedir}"/init
    (cd "${vmpreparedir}" && tar -czf init.tar.gz --remove-files init)
    supermin --build "${vmpreparedir}" --size 5G -f ext2 -o "${vmbuilddir}"

    echo "$@" > "${TMPDIR}"/cmd.sh

    # support local dev cases where src/config is a symlink
    srcvirtfs=()
    if [ -L "${workdir}/src/config" ]; then
        # qemu follows symlinks
        srcvirtfs=("-virtfs" "local,id=source,path=${workdir}/src/config,security_model=none,mount_tag=source")
    fi

    qemu-kvm -nodefaults -nographic -m 2048 -no-reboot \
        -kernel "${vmbuilddir}/kernel" \
        -initrd "${vmbuilddir}/initrd" \
        -netdev user,id=eth0,hostname=supermin \
        -device virtio-net-pci,netdev=eth0 \
        -device virtio-scsi-pci,id=scsi0,bus=pci.0,addr=0x3 \
        -drive if=none,id=drive-scsi0-0-0-0,snapshot=on,file="${vmbuilddir}/root" \
        -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=0,drive=drive-scsi0-0-0-0,id=scsi0-0-0-0,bootindex=1 \
        -drive if=none,id=drive-scsi0-0-0-1,discard=unmap,file="${workdir}/cache/cache.qcow2" \
        -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=1,drive=drive-scsi0-0-0-1,id=scsi0-0-0-1 \
        -virtfs local,id=workdir,path="${workdir}",security_model=none,mount_tag=workdir \
        "${srcvirtfs[@]}" -serial stdio -append "root=/dev/sda console=ttyS0 selinux=1 enforcing=0 autorelabel=1"

    if [ ! -f "${workdir}"/tmp/rc ]; then
        fatal "Couldn't find rc file, something went terribly wrong!"
    fi
    return "$(cat "${workdir}"/tmp/rc)"
}
